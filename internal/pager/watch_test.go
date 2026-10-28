package pager

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testInterval = 5 * time.Millisecond

func watchedDocument(t *testing.T, content string, byName bool) (*Document, string, *os.File) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "log")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	doc := NewDocument()
	if _, err := doc.ReadFrom(file); err != nil {
		t.Fatal(err)
	}
	if !doc.FileBacked() {
		t.Fatal("regular file is not file-backed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		doc.Watch(ctx, file, path, byName, testInterval)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
		doc.Close()
		file.Close()
	})
	return doc, path, file
}

func waitContent(t *testing.T, doc *Document, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		got := make([]byte, doc.Len())
		n, err := doc.ReadAt(got, 0)
		if (err == nil || errors.Is(err, io.EOF)) && string(got[:n]) == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("content = %q (%v), want %q", got[:n], err, want)
		}
		time.Sleep(testInterval)
	}
}

func appendFile(t *testing.T, path, text string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.WriteString(text); err != nil {
		t.Fatal(err)
	}
}

func TestWatchAppendsAndTruncation(t *testing.T) {
	doc, path, _ := watchedDocument(t, "one\n", false)
	appendFile(t, path, "two\n")
	waitContent(t, doc, "one\ntwo\n")
	if generation, _ := doc.Generation(); generation != 0 {
		t.Fatalf("append started generation %d", generation)
	}
	if err := os.Truncate(path, 0); err != nil {
		t.Fatal(err)
	}
	// A read racing the truncation reports it instead of stale bytes.
	if _, err := doc.ReadAt(make([]byte, 8), 0); err != nil && !errors.Is(err, ErrTruncated) && !errors.Is(err, io.EOF) {
		t.Fatalf("racing read = %v", err)
	}
	appendFile(t, path, "new\n")
	waitContent(t, doc, "new\n")
	if generation, reason := doc.Generation(); generation != 1 || reason != "truncated" {
		t.Fatalf("generation = %d %q", generation, reason)
	}
}

func TestWatchDetectsRewriteLongerThanBefore(t *testing.T) {
	doc, path, _ := watchedDocument(t, "short\n", false)
	// Truncating and rewriting within one poll only looks like growth.
	file, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteAt([]byte("rewritten and longer\n"), 0); err != nil {
		t.Fatal(err)
	}
	file.Close()
	waitContent(t, doc, "rewritten and longer\n")
	if generation, reason := doc.Generation(); generation != 1 || reason != "truncated" {
		t.Fatalf("generation = %d %q", generation, reason)
	}
}

func TestWatchByDescriptorKeepsRotatedFile(t *testing.T) {
	doc, path, _ := watchedDocument(t, "old\n", false)
	if err := os.Rename(path, path+".1"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("fresh\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	appendFile(t, path+".1", "late\n")
	waitContent(t, doc, "old\nlate\n")
}

func TestWatchByNameFollowsRotationAndMissingFile(t *testing.T) {
	doc, path, _ := watchedDocument(t, "old\n", true)
	if err := os.Rename(path, path+".1"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(doc.WatchStatus(), "waiting for it to return") {
		if time.Now().After(deadline) {
			t.Fatalf("missing file status = %q", doc.WatchStatus())
		}
		time.Sleep(testInterval)
	}
	waitContent(t, doc, "old\n")
	if err := os.WriteFile(path, []byte("fresh\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitContent(t, doc, "fresh\n")
	if _, reason := doc.Generation(); reason != "replaced" || doc.WatchStatus() != "" {
		t.Fatalf("reason %q status %q", reason, doc.WatchStatus())
	}
	appendFile(t, path, "more\n")
	waitContent(t, doc, "fresh\nmore\n")
}

func TestStreamsAreNotFileBacked(t *testing.T) {
	doc := NewDocument()
	defer doc.Close()
	doc.ReadFrom(strings.NewReader("pipe"))
	if doc.FileBacked() {
		t.Fatal("stream reported as file-backed")
	}
}
