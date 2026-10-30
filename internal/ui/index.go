package ui

import (
	"encoding/binary"
	"fmt"

	"glimpse/internal/pager"
)

// metadataQuota bounds the bytes of a group of indexes together, so a sparse
// index does not hold back quota that a dense one needs.
type metadataQuota struct{ limit, used int64 }

// offsetIndex has a bounded in-memory tail plus one 1 KiB read page. Older
// records live in a private temporary file, subject to a separate hard quota.
// It never retains a Go object or slice header per spilled record.
type offsetIndex struct {
	tail           []int64
	limit          int
	quota          *metadataQuota
	count, flushed int
	file           *pager.TempFile
	dir            string // where the file is created; empty selects pager.TempDir
	page           [1024]byte
	pageOffset     int64
	pageLength     int
	err            error
}

func newOffsetIndex(memory int64, quota *metadataQuota, dir string) *offsetIndex {
	limit := int(max(1, (memory-1024)/8))
	// The tail grows on demand, so a small input does not allocate its budget.
	return &offsetIndex{tail: make([]int64, 0, min(limit, 1024)), limit: limit, quota: quota, pageOffset: -1, dir: dir}
}

func (i *offsetIndex) append(value int64) error {
	if i.err != nil {
		return i.err
	}
	if i.quota.used+8 > i.quota.limit {
		i.err = fmt.Errorf("metadata quota reached; increase storage.metadata-quota")
		return i.err
	}
	if len(i.tail) == i.limit {
		if i.file == nil {
			i.file, i.err = pager.CreateTemp(i.dir, "glimpse-index-*")
			if i.err != nil {
				return i.err
			}
		}
		// Write in large blocks; the buffer lives only for this flush, which
		// happens once per limit appends, so it stays outside the budget.
		buffer := make([]byte, min(len(i.tail)*8, 64<<10))
		for start := 0; start < len(i.tail); {
			n := min(len(i.tail)-start, len(buffer)/8)
			for j := 0; j < n; j++ {
				binary.LittleEndian.PutUint64(buffer[j*8:], uint64(i.tail[start+j]))
			}
			_, i.err = i.file.WriteAt(buffer[:n*8], int64(i.flushed+start)*8)
			if i.err != nil {
				return i.err
			}
			start += n
		}
		i.flushed += len(i.tail)
		i.tail = i.tail[:0]
		i.pageOffset = -1
	}
	if len(i.tail) == cap(i.tail) {
		capacity := min(i.limit, max(16, cap(i.tail)*2))
		tail := make([]int64, len(i.tail), capacity)
		copy(tail, i.tail)
		i.tail = tail
	}
	i.tail = append(i.tail, value)
	i.count++
	i.quota.used += 8
	return nil
}

func (i *offsetIndex) at(index int) int64 {
	if index < 0 || index >= i.count || i.err != nil {
		return 0
	}
	if index >= i.flushed {
		return i.tail[index-i.flushed]
	}
	offset := int64(index) * 8
	page := offset / int64(len(i.page)) * int64(len(i.page))
	if i.pageOffset != page {
		length := int(min(int64(len(i.page)), int64(i.flushed)*8-page))
		i.pageLength, i.err = i.file.ReadAt(i.page[:length], page)
		if i.err != nil {
			return 0
		}
		i.pageOffset = page
	}
	if offset-i.pageOffset+8 > int64(i.pageLength) {
		i.err = fmt.Errorf("short metadata index page")
		return 0
	}
	return int64(binary.LittleEndian.Uint64(i.page[offset-i.pageOffset:]))
}

func (i *offsetIndex) close() error {
	if i.file == nil {
		return nil
	}
	err := i.file.Close()
	i.file = nil
	return err
}

func (i *offsetIndex) truncate(count int) error {
	if count < 0 || count > i.count {
		return fmt.Errorf("invalid index rewind")
	}
	if count >= i.flushed {
		i.tail = i.tail[:count-i.flushed]
	} else {
		if err := i.file.Truncate(int64(count) * 8); err != nil {
			return err
		}
		i.flushed, i.tail = count, i.tail[:0]
	}
	i.quota.used -= int64(i.count-count) * 8
	i.count, i.pageOffset = count, -1
	return nil
}
