//go:build unix

package pager

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestWatchByNameIgnoresFIFOReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log")
	if err := os.WriteFile(path, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	doc := NewDocument()
	defer doc.Close()
	if _, err := doc.ReadFrom(file); err != nil {
		t.Fatal(err)
	}
	fifo := path + ".fifo"
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skip("no FIFOs:", err)
	}
	if err := os.Rename(fifo, path); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		doc.Watch(ctx, file, path, true, testInterval)
	}()
	time.Sleep(20 * testInterval)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		// Unblock the stuck open so the test can finish.
		if writer, err := os.OpenFile(path, os.O_WRONLY, 0); err == nil {
			writer.Close()
		}
		<-done
		t.Fatal("Watch blocked opening a FIFO and ignored cancellation")
	}
	waitContent(t, doc, "old\n")
}
