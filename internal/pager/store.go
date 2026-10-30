package pager

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
)

var errNegativeOffset = errors.New("negative byte offset")
var ErrEvicted = errors.New("requested bytes were discarded by rolling retention")

// ErrTruncated reports that a backing file shrank below the bytes it promised;
// a file watcher reloads the document shortly afterward.
var ErrTruncated = fmt.Errorf("backing file was truncated: %w", io.ErrUnexpectedEOF)

type StorageOptions struct {
	MemoryBudget int64
	SpoolQuota   int64
	Retention    string
	SpoolDir     string
}

func DefaultStorageOptions() StorageOptions {
	return StorageOptions{MemoryBudget: 64 << 20, SpoolQuota: 1 << 30, Retention: "lossless"}
}

// Store bounds resident bytes. Lossless streams spill to a private temporary
// file and wait at quota; regular files are reread directly. Rolling retention
// is opt-in, uses a fixed ring, and keeps absolute offsets after eviction.
type Store struct {
	mu                     sync.Mutex
	changed                *sync.Cond
	chunkSize              int
	chunkCapacity          int
	chunks                 [][]byte
	length, base, resident int64
	options                StorageOptions
	spool                  *TempFile
	external               io.ReaderAt
	externalOffset         int64
	ring                   []byte
	closed, paused         bool
	generation             int64  // incremented whenever the content is replaced
	reloadReason           string // why the current generation was loaded
}

func NewStore(chunkSize int) *Store {
	s, err := NewStoreWithOptions(chunkSize, DefaultStorageOptions())
	if err != nil {
		panic(err)
	}
	return s
}

func NewStoreWithOptions(chunkSize int, options StorageOptions) (*Store, error) {
	if chunkSize <= 0 {
		return nil, fmt.Errorf("chunk-size must be positive")
	}
	if options.Retention == "" {
		options.Retention = "lossless"
	}
	if options.Retention != "lossless" && options.Retention != "rolling" {
		return nil, fmt.Errorf("retention must be lossless or rolling")
	}
	if options.MemoryBudget < 64 || options.MemoryBudget > int64(int(^uint(0)>>1)) {
		return nil, fmt.Errorf("memory-budget must be at least 64 bytes and fit in memory")
	}
	if options.SpoolQuota < options.MemoryBudget {
		return nil, fmt.Errorf("spool-quota must be at least memory-budget")
	}
	s := &Store{chunkSize: chunkSize, options: options}
	s.chunkCapacity = int(min(int64(chunkSize), options.MemoryBudget-24))
	if options.Retention == "lossless" {
		capacity := min(int64(chunkSize), options.MemoryBudget-24)
		headers := min(int64(16), options.MemoryBudget/(capacity+24))
		s.chunks = make([][]byte, 0, int(headers))
		s.resident = headers * 24
	}
	s.changed = sync.NewCond(&s.mu)
	return s, nil
}

func (s *Store) useFile(file io.ReaderAt, offset, length int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return os.ErrClosed
	}
	if s.length != 0 {
		return fmt.Errorf("cannot replace a nonempty document")
	}
	s.external, s.externalOffset, s.length = file, offset, length
	s.chunks, s.resident = nil, 0
	return nil
}

// grow extends a file-backed snapshot to length bytes after its offset.
func (s *Store) grow(length int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.external != nil && length > s.length {
		s.length = length
	}
}

// reload replaces a file-backed snapshot's content, starting a new generation.
func (s *Store) reload(file io.ReaderAt, offset, length int64, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return os.ErrClosed
	}
	s.external, s.externalOffset, s.length = file, offset, length
	s.generation++
	s.reloadReason = reason
	return nil
}

func (s *Store) fileState() (io.ReaderAt, int64, int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.external, s.externalOffset, s.length
}

func (s *Store) Generation() (int64, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.generation, s.reloadReason
}

func (s *Store) spill() error {
	file, err := CreateTemp(s.options.SpoolDir, "glimpse-spool-*")
	if err != nil {
		return fmt.Errorf("create input spool: %w", err)
	}
	for _, chunk := range s.chunks {
		if _, err := file.Write(chunk); err != nil {
			file.Close()
			return fmt.Errorf("write input spool: %w", err)
		}
	}
	s.spool, s.chunks, s.resident = file, nil, 0
	return nil
}

func (s *Store) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, os.ErrClosed
	}
	if s.external != nil {
		return 0, fmt.Errorf("cannot append to a file-backed snapshot")
	}
	if s.options.Retention == "rolling" {
		if len(s.ring) == 0 {
			s.ring = make([]byte, int(s.options.MemoryBudget))
			s.resident = int64(len(s.ring))
		}
		n := len(p)
		if int64(len(p)) > int64(len(s.ring)) {
			p = p[len(p)-len(s.ring):]
		}
		position := (s.length + int64(n-len(p))) % int64(len(s.ring))
		first := copy(s.ring[int(position):], p)
		copy(s.ring, p[first:])
		s.length += int64(n)
		s.base = max(0, s.length-int64(len(s.ring)))
		return n, nil
	}
	written := 0
	for len(p) > 0 {
		for s.length >= s.options.SpoolQuota && !s.closed {
			s.paused = true
			s.changed.Wait()
		}
		s.paused = false
		if s.closed {
			return written, os.ErrClosed
		}
		n := int(min(int64(len(p)), s.options.SpoolQuota-s.length))
		if s.spool == nil {
			if len(s.chunks) == 0 || len(s.chunks[len(s.chunks)-1]) == cap(s.chunks[len(s.chunks)-1]) {
				capacity := int(min(int64(s.chunkSize), s.options.MemoryBudget-24))
				// Include the slice headers in the resident memory accounting.
				if len(s.chunks) == cap(s.chunks) {
					raw := s.resident - int64(cap(s.chunks))*24
					capacityLimit := (s.options.MemoryBudget - raw - int64(capacity)) / 24
					headers := min(int64(cap(s.chunks)*2), capacityLimit)
					if headers > int64(cap(s.chunks)) {
						chunks := make([][]byte, len(s.chunks), int(headers))
						copy(chunks, s.chunks)
						s.resident += (headers - int64(cap(s.chunks))) * 24
						s.chunks = chunks
					}
				}
				if s.resident+int64(capacity) > s.options.MemoryBudget || len(s.chunks) == cap(s.chunks) {
					if err := s.spill(); err != nil {
						return written, err
					}
				} else {
					s.chunks = append(s.chunks, make([]byte, 0, capacity))
					s.resident += int64(capacity)
				}
			}
		}
		if s.spool != nil {
			count, err := s.spool.WriteAt(p[:n], s.length)
			s.length += int64(count)
			written += count
			if err != nil {
				return written, fmt.Errorf("write input spool: %w", err)
			}
			p = p[count:]
		} else {
			last := len(s.chunks) - 1
			n = min(n, cap(s.chunks[last])-len(s.chunks[last]))
			s.chunks[last] = append(s.chunks[last], p[:n]...)
			s.length += int64(n)
			written += n
			p = p[n:]
		}
	}
	return written, nil
}

func (s *Store) Len() int64         { s.mu.Lock(); defer s.mu.Unlock(); return s.length }
func (s *Store) Start() int64       { s.mu.Lock(); defer s.mu.Unlock(); return s.base }
func (s *Store) MemoryBytes() int64 { s.mu.Lock(); defer s.mu.Unlock(); return s.resident }
func (s *Store) Paused() bool       { s.mu.Lock(); defer s.mu.Unlock(); return s.paused }

func (s *Store) SetQuota(quota int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if quota < max(s.length, s.options.MemoryBudget) {
		return fmt.Errorf("spool quota cannot be smaller than retained input or memory budget")
	}
	s.options.SpoolQuota = quota
	s.changed.Broadcast()
	return nil
}

func (s *Store) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, errNegativeOffset
	}
	if len(p) == 0 {
		return 0, nil
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return 0, os.ErrClosed
	}
	if s.external == nil || off < s.base || off >= s.length {
		defer s.mu.Unlock()
		return s.readAt(p, off)
	}
	// Read the file without holding the lock, so that parallel scans don't
	// take turns in the system call. A reload swaps in another file under a
	// new generation; readers of the old one may then fail, but the results
	// of an old generation are discarded.
	file, position, length := s.external, s.externalOffset+off, s.length
	s.mu.Unlock()
	return readExternal(file, p, position, int(min(int64(len(p)), length-off)))
}

// readExternal reads want bytes of a file-backed snapshot into p.
func readExternal(file io.ReaderAt, p []byte, position int64, want int) (int, error) {
	n, err := file.ReadAt(p[:want], position)
	if err != nil && !errors.Is(err, io.EOF) {
		return n, err
	}
	if n < want {
		return n, ErrTruncated
	}
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func (s *Store) readAt(p []byte, off int64) (int, error) {
	if off < s.base {
		return 0, ErrEvicted
	}
	if off >= s.length {
		return 0, io.EOF
	}
	want := int(min(int64(len(p)), s.length-off))
	n := 0
	if s.external != nil {
		return readExternal(s.external, p, s.externalOffset+off, want)
	} else if s.spool != nil {
		var err error
		n, err = s.spool.ReadAt(p[:want], off)
		if err != nil && !errors.Is(err, io.EOF) {
			return n, err
		}
		if n < want {
			return n, io.ErrUnexpectedEOF
		}
	} else if len(s.ring) > 0 {
		position := int(off % int64(len(s.ring)))
		n = copy(p[:want], s.ring[position:])
		n += copy(p[n:want], s.ring[:want-n])
	} else {
		index, position := int(off/int64(s.chunkCapacity)), int(off%int64(s.chunkCapacity))
		for n < want && index < len(s.chunks) {
			count := copy(p[n:want], s.chunks[index][position:])
			n += count
			index++
			position = 0
		}
	}
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func (s *Store) Tail(maxBytes int) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	if maxBytes <= 0 || s.closed {
		return nil
	}
	n := min(int64(maxBytes), s.length-s.base)
	result := make([]byte, int(n))
	s.readAt(result, s.length-n)
	return result
}

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	s.changed.Broadcast()
	s.chunks, s.ring, s.resident = nil, nil, 0
	if s.spool == nil {
		return nil
	}
	return s.spool.Close()
}
