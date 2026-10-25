package pager

import (
	"errors"
	"io"
	"sync"
)

var errNegativeOffset = errors.New("negative byte offset")

// Store keeps an append-only byte stream in fixed-capacity chunks. Chunks
// don't correspond to lines, so a single line can span any number of them.
// TODO: define a retention policy (disk spool, bounded rolling window, etc.)
// before using this for an unbounded stream in production.
type Store struct {
	mu        sync.RWMutex
	chunkSize int
	chunks    [][]byte
	length    int64
}

// NewStore creates a byte store with the requested chunk size.
func NewStore(chunkSize int) *Store {
	if chunkSize <= 0 {
		panic("pager: chunk size must be positive")
	}
	return &Store{chunkSize: chunkSize}
}

// Write appends p, copying it into chunks owned by the store.
func (s *Store) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	remaining := p
	for len(remaining) > 0 {
		if len(s.chunks) == 0 || len(s.chunks[len(s.chunks)-1]) == s.chunkSize {
			s.chunks = append(s.chunks, make([]byte, 0, s.chunkSize))
		}
		last := len(s.chunks) - 1
		space := s.chunkSize - len(s.chunks[last])
		n := min(space, len(remaining))
		s.chunks[last] = append(s.chunks[last], remaining[:n]...)
		remaining = remaining[n:]
		s.length += int64(n)
	}
	return len(p), nil
}

// Len returns the total number of bytes appended.
func (s *Store) Len() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.length
}

// ReadAt copies data starting at off. It follows io.ReaderAt's EOF behavior
// when fewer than len(p) bytes are currently available.
func (s *Store) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, errNegativeOffset
	}
	if len(p) == 0 {
		return 0, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if off >= s.length {
		return 0, io.EOF
	}

	want := min(int64(len(p)), s.length-off)
	read := 0
	for int64(read) < want {
		position := off + int64(read)
		chunkIndex := int(position / int64(s.chunkSize))
		chunkOffset := int(position % int64(s.chunkSize))
		n := copy(p[read:int(want)], s.chunks[chunkIndex][chunkOffset:])
		read += n
	}
	if read < len(p) {
		return read, io.EOF
	}
	return read, nil
}

// Tail returns a copy of the last maxBytes bytes, or all bytes if the store is
// shorter. A fresh copy keeps callers from observing mutable chunk storage.
func (s *Store) Tail(maxBytes int) []byte {
	if maxBytes <= 0 {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	length := min(int64(maxBytes), s.length)
	result := make([]byte, int(length))
	off := s.length - length
	read := 0
	for int64(read) < length {
		position := off + int64(read)
		chunkIndex := int(position / int64(s.chunkSize))
		chunkOffset := int(position % int64(s.chunkSize))
		n := copy(result[read:], s.chunks[chunkIndex][chunkOffset:])
		read += n
	}
	return result
}
