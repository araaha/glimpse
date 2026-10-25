package ui

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
)

// offsetIndex has a bounded in-memory tail plus one 4 KiB read page. Older
// records live in a private temporary file, subject to a separate hard quota.
// It never retains a Go object or slice header per spilled record.
type offsetIndex struct {
	tail           []int64
	limit          int
	quota          int64
	count, flushed int
	file           *os.File
	page           [4096]byte
	pageOffset     int64
	pageLength     int
	err            error
}

func newOffsetIndex(memory, quota int64) *offsetIndex {
	limit := int(max(1, (memory-4096)/8))
	return &offsetIndex{tail: make([]int64, 0, limit), limit: limit, quota: quota, pageOffset: -1}
}

func (i *offsetIndex) append(value int64) error {
	if i.err != nil {
		return i.err
	}
	if int64(i.count+1)*8 > i.quota {
		i.err = fmt.Errorf("metadata quota reached; increase storage.metadata-quota")
		return i.err
	}
	if len(i.tail) == i.limit {
		if i.file == nil {
			i.file, i.err = os.CreateTemp("", "glimpse-index-*")
			if i.err != nil {
				return i.err
			}
		}
		for start := 0; start < len(i.tail); {
			n := min(len(i.tail)-start, len(i.page)/8)
			for j := 0; j < n; j++ {
				binary.LittleEndian.PutUint64(i.page[j*8:], uint64(i.tail[start+j]))
			}
			_, i.err = i.file.WriteAt(i.page[:n*8], int64(i.flushed+start)*8)
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
	name := i.file.Name()
	err := i.file.Close()
	removeErr := os.Remove(name)
	i.file = nil
	return errors.Join(err, removeErr)
}
