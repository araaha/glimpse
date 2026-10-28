package pager

import (
	"io"
	"os"
	"sync"
	"time"
)

// DefaultChunkSize is the allocation unit for incoming bytes.
const DefaultChunkSize = 64 * 1024

// Document stores the input as byte chunks, independently of line boundaries.
// This lets an unfinished (or arbitrarily long) line become available as it
// arrives instead of waiting for a newline.
type Document struct {
	data        *Store
	watchMu     sync.Mutex
	watchStatus string
	loaded      []byte // fingerprint of a file-backed document when loaded
	// loadedModTime is the file's modification time when it was loaded.
	loadedModTime time.Time
}

// NewDocument creates an empty document.
func NewDocument() *Document {
	return NewDocumentWithChunkSize(DefaultChunkSize)
}

// NewDocumentWithChunkSize creates a document with a positive chunk size.
func NewDocumentWithChunkSize(chunkSize int) *Document {
	return &Document{data: NewStore(chunkSize)}
}

// Write appends input bytes to the document. It implements io.Writer.
func (d *Document) Write(p []byte) (int, error) {
	return d.data.Write(p)
}

// ReadFrom streams all bytes from r into the document.
func (d *Document) ReadFrom(r io.Reader) (int64, error) {
	if file, ok := r.(*os.File); ok && d.data.options.Retention != "rolling" {
		if info, err := file.Stat(); err == nil && info.Mode().IsRegular() {
			if offset, err := file.Seek(0, io.SeekCurrent); err == nil {
				length := max(0, info.Size()-offset)
				if err := d.data.useFile(file, offset, length); err != nil {
					return 0, err
				}
				d.loaded, d.loadedModTime = sample(file, offset, length), info.ModTime()
				_, err := file.Seek(length, io.SeekCurrent)
				return length, err
			}
		}
	}
	// Copy to the underlying writer: copying to d would invoke ReadFrom again
	// for readers without a WriterTo method, recursing instead of reading.
	return io.Copy(d.data, r)
}

func NewDocumentWithOptions(chunkSize int, options StorageOptions) (*Document, error) {
	store, err := NewStoreWithOptions(chunkSize, options)
	if err != nil {
		return nil, err
	}
	return &Document{data: store}, nil
}

func (d *Document) Start() int64 { return d.data.Start() }

// RetainsAll reports whether old bytes remain readable for the document's life.
func (d *Document) RetainsAll() bool           { return d.data.options.Retention != "rolling" }
func (d *Document) MemoryBytes() int64         { return d.data.MemoryBytes() }
func (d *Document) Paused() bool               { return d.data.Paused() }
func (d *Document) SetQuota(quota int64) error { return d.data.SetQuota(quota) }
func (d *Document) Close() error               { return d.data.Close() }

// Len reports the total number of bytes ingested so far.
func (d *Document) Len() int64 {
	return d.data.Len()
}

// ReadAt reads bytes at an absolute offset. It is safe to read while input is
// still being appended; EOF means the requested bytes have not arrived (or the
// offset is beyond the current end), not necessarily that the input has ended.
func (d *Document) ReadAt(p []byte, off int64) (int, error) {
	return d.data.ReadAt(p, off)
}

// Tail returns a copy of up to maxBytes most recently ingested bytes. The
// copy allows rendering without holding the store lock. Rendering should
// request only the bytes needed for the current viewport.
func (d *Document) Tail(maxBytes int) []byte {
	return d.data.Tail(maxBytes)
}
