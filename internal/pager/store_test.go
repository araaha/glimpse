package pager

import (
	"bytes"
	"errors"
	"io"
	"os"
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

// TestStoreReadAtContract checks the io.ReaderAt edge cases for each kind of
// backing: chunks in memory, a spool file, a rolling ring, and a regular file.
func TestStoreReadAtContract(t *testing.T) {
	input := bytes.Repeat([]byte("0123456789abcdef"), 13)
	spool, err := NewStoreWithOptions(4, StorageOptions{MemoryBudget: 64, SpoolQuota: 1024, Retention: "lossless", SpoolDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	rolling, err := NewStoreWithOptions(4, StorageOptions{MemoryBudget: 64, SpoolQuota: 64, Retention: "rolling"})
	if err != nil {
		t.Fatal(err)
	}
	file := NewStore(4)
	if err := file.useFile(bytes.NewReader(append([]byte("header"), input...)), 6, int64(len(input))); err != nil {
		t.Fatal(err)
	}
	for name, store := range map[string]*Store{"memory": NewStore(4), "spool": spool, "rolling": rolling, "file": file} {
		if name != "file" {
			if n, err := store.Write(input); err != nil || n != len(input) {
				t.Fatalf("%s: Write = %d, %v", name, n, err)
			}
		}
		base := store.Start()
		if name == "rolling" && base == 0 {
			t.Fatal("rolling store retained everything")
		}
		buf := make([]byte, 4)
		if n, err := store.ReadAt(buf, -1); n != 0 || err == nil {
			t.Errorf("%s: negative offset = %d, %v", name, n, err)
		}
		if n, err := store.ReadAt(nil, base); n != 0 || err != nil {
			t.Errorf("%s: empty read = %d, %v", name, n, err)
		}
		if n, err := store.ReadAt(buf, base); n != 4 || err != nil || string(buf) != string(input[base:base+4]) {
			t.Errorf("%s: read at base = %d %q, %v", name, n, buf[:n], err)
		}
		if n, err := store.ReadAt(buf, store.Len()-2); n != 2 || err != io.EOF || string(buf[:n]) != "ef" {
			t.Errorf("%s: short tail = %d %q, %v", name, n, buf[:n], err)
		}
		if n, err := store.ReadAt(buf, store.Len()); n != 0 || err != io.EOF {
			t.Errorf("%s: read at end = %d, %v", name, n, err)
		}
		if base > 0 {
			if _, err := store.ReadAt(buf, base-1); !errors.Is(err, ErrEvicted) {
				t.Errorf("%s: evicted read = %v", name, err)
			}
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := store.ReadAt(buf, base); !errors.Is(err, os.ErrClosed) {
			t.Errorf("%s: read after close = %v", name, err)
		}
		if _, err := store.Write([]byte("x")); err == nil {
			t.Errorf("%s: write after close succeeded", name)
		}
	}
}

func TestFileBackedStoreRefusesWrites(t *testing.T) {
	store := NewStore(4)
	defer store.Close()
	if err := store.useFile(bytes.NewReader([]byte("abcdef")), 0, 6); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Write([]byte("x")); err == nil {
		t.Fatal("appended to a file-backed snapshot")
	}
	if err := store.useFile(bytes.NewReader(nil), 0, 0); err == nil {
		t.Fatal("replaced a nonempty document")
	}
}
