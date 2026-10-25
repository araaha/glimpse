package pager

import "io"

// DefaultChunkSize is the allocation unit for incoming bytes.
const DefaultChunkSize = 64 * 1024

// Document stores the input as byte chunks, independently of line boundaries.
// This lets an unfinished (or arbitrarily long) line become available as it
// arrives instead of waiting for a newline.
type Document struct {
	data *Store
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
	// Copy to the underlying writer: copying to d would invoke ReadFrom again
	// for readers without a WriterTo method, recursing instead of reading.
	return io.Copy(d.data, r)
}

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
