package pager

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLosslessSpillAndCleanup(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStoreWithOptions(32, StorageOptions{MemoryBudget: 256, SpoolQuota: 2048, SpoolDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	want := strings.Repeat("abcdefg", 100)
	for start := 0; start < len(want); {
		end := min(len(want), start+19)
		if _, err := s.Write([]byte(want[start:end])); err != nil {
			t.Fatal(err)
		}
		if s.MemoryBytes() > 256 {
			t.Fatalf("resident bytes = %d", s.MemoryBytes())
		}
		start = end
	}
	got := make([]byte, len(want))
	if n, err := s.ReadAt(got, 0); n != len(want) || err != nil || string(got) != want {
		t.Fatalf("spooled input = %d bytes, %v", n, err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("spool entries = %v", entries)
	}
	info, _ := entries[0].Info()
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("spool permissions = %v", info.Mode())
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	entries, _ = os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatal("spool leaked after close")
	}
}

func waitPaused(t *testing.T, s *Store) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for !s.Paused() {
		select {
		case <-deadline:
			t.Fatal("writer did not pause at quota")
		case <-tick.C:
		}
	}
}

func TestQuotaBackpressureResumeAndClose(t *testing.T) {
	for _, resume := range []bool{false, true} {
		s, err := NewStoreWithOptions(32, StorageOptions{MemoryBudget: 128, SpoolQuota: 256, SpoolDir: t.TempDir()})
		if err != nil {
			t.Fatal(err)
		}
		result := make(chan error, 1)
		go func() { _, err := s.Write(bytes.Repeat([]byte{'x'}, 300)); result <- err }()
		waitPaused(t, s)
		if s.Len() != 256 {
			t.Fatalf("quota exceeded: %d", s.Len())
		}
		data := make([]byte, 8)
		if _, err := s.ReadAt(data, 0); err != nil {
			t.Fatalf("paused store unreadable: %v", err)
		}
		if resume {
			if err := s.SetQuota(512); err != nil {
				t.Fatal(err)
			}
		} else {
			s.Close()
		}
		select {
		case err := <-result:
			if resume && err != nil || !resume && !errors.Is(err, os.ErrClosed) {
				t.Fatalf("writer result: %v", err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("blocked writer did not wake")
		}
		s.Close()
	}
}

func TestRollingOffsetsAndExplicitEviction(t *testing.T) {
	s, _ := NewStoreWithOptions(16, StorageOptions{MemoryBudget: 64, SpoolQuota: 128, Retention: "rolling"})
	defer s.Close()
	input := []byte(strings.Repeat("0123456789", 15))
	s.Write(input[:100])
	s.Write(input[100:])
	if s.Start() != 86 || s.Len() != 150 || s.MemoryBytes() != 64 {
		t.Fatalf("rolling bounds: %d/%d resident %d", s.Start(), s.Len(), s.MemoryBytes())
	}
	if _, err := s.ReadAt(make([]byte, 1), 0); !errors.Is(err, ErrEvicted) {
		t.Fatalf("evicted read = %v", err)
	}
	got := make([]byte, 64)
	if _, err := s.ReadAt(got, 86); err != nil || !bytes.Equal(got, input[86:]) {
		t.Fatalf("ring content %q: %v", got, err)
	}
	if !bytes.Equal(s.Tail(1000), input[86:]) {
		t.Fatal("tail included discarded input")
	}
}

func TestRegularFileRereadsWithoutSpooling(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "input")
	want := strings.Repeat("abcdefgh", 1024)
	if err := os.WriteFile(path, []byte(want), 0o600); err != nil {
		t.Fatal(err)
	}
	file, _ := os.Open(path)
	defer file.Close()
	file.Seek(3, io.SeekStart)
	doc, _ := NewDocumentWithOptions(32, StorageOptions{MemoryBudget: 128, SpoolQuota: 256, SpoolDir: dir})
	defer doc.Close()
	if n, err := doc.ReadFrom(file); err != nil || n != int64(len(want)-3) {
		t.Fatalf("file attach = %d, %v", n, err)
	}
	if doc.MemoryBytes() != 0 {
		t.Fatalf("file retained %d bytes", doc.MemoryBytes())
	}
	got := make([]byte, 20)
	if _, err := doc.ReadAt(got, 4000); err != nil || string(got) != want[4003:4023] {
		t.Fatalf("file reread = %q, %v", got, err)
	}
	position, _ := file.Seek(0, io.SeekCurrent)
	if position != int64(len(want)) {
		t.Fatal("ReaderFrom did not advance source")
	}
	file.Close()
	if _, err := doc.ReadAt(got, 0); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("file read failure hidden: %v", err)
	}
}

func TestStorageOptionValidationAndSpoolFailure(t *testing.T) {
	for _, options := range []StorageOptions{{MemoryBudget: 0, SpoolQuota: 100}, {MemoryBudget: 128, SpoolQuota: 64}, {MemoryBudget: 64, SpoolQuota: 128, Retention: "unknown"}} {
		if _, err := NewStoreWithOptions(8, options); err == nil {
			t.Fatalf("accepted %+v", options)
		}
	}
	s, _ := NewStoreWithOptions(32, StorageOptions{MemoryBudget: 64, SpoolQuota: 256, SpoolDir: filepath.Join(t.TempDir(), "missing")})
	defer s.Close()
	if _, err := s.Write(make([]byte, 128)); err == nil {
		t.Fatal("spool error ignored")
	}
}
