package pager

import (
	"bytes"
	"io"
	"testing"
)

func TestStoreChunksInputIndependentOfLines(t *testing.T) {
	store := NewStore(4)
	input := []byte("abcdefghij") // no newline; spans multiple chunks
	if n, err := store.Write(input); err != nil || n != len(input) {
		t.Fatalf("Write() = (%d, %v), want (%d, nil)", n, err, len(input))
	}

	got := store.Tail(len(input))
	if !bytes.Equal(got, input) {
		t.Fatalf("Tail() = %q, want %q", got, input)
	}
	if gotLen := store.Len(); gotLen != int64(len(input)) {
		t.Fatalf("Len() = %d, want %d", gotLen, len(input))
	}
}

func TestStoreReadAtAcrossChunks(t *testing.T) {
	store := NewStore(3)
	_, _ = store.Write([]byte("abcdefgh"))
	buf := make([]byte, 5)
	n, err := store.ReadAt(buf, 2)
	if err != nil || n != 5 || string(buf) != "cdefg" {
		t.Fatalf("ReadAt() = (%d, %q, %v), want (5, %q, nil)", n, buf, err, "cdefg")
	}

	n, err = store.ReadAt(buf, 6)
	if n != 2 || err != io.EOF || string(buf[:n]) != "gh" {
		t.Fatalf("ReadAt() at tail = (%d, %q, %v), want (2, %q, EOF)", n, buf[:n], err, "gh")
	}
}

func TestDocumentReadFromPublishesUnterminatedInput(t *testing.T) {
	doc := NewDocument()
	input := bytes.NewBufferString("continuous input without newline")
	wantLen := input.Len()
	n, err := doc.ReadFrom(input)
	if err != nil || n != int64(wantLen) {
		t.Fatalf("ReadFrom() = (%d, %v), want (%d, nil)", n, err, wantLen)
	}
	if got := string(doc.Tail(int(n))); got != "continuous input without newline" {
		t.Fatalf("Tail() = %q", got)
	}
}

func TestDocumentReadFromReaderWithoutWriterTo(t *testing.T) {
	// A pipe-like reader must not recursively invoke Document.ReadFrom through
	// io.Copy's ReaderFrom fast path.
	input := struct{ io.Reader }{bytes.NewBufferString("pipe input")}
	doc := NewDocument()
	n, err := doc.ReadFrom(input)
	if err != nil || n != 10 || string(doc.Tail(10)) != "pipe input" {
		t.Fatalf("ReadFrom() = (%d, %v), data = %q", n, err, doc.Tail(10))
	}
}
