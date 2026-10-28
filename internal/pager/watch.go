package pager

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"time"
)

// FileBacked reports whether the document reads a regular file in place, so
// later changes to that file can be followed.
func (d *Document) FileBacked() bool {
	external, _, _ := d.data.fileState()
	return external != nil
}

// Generation increases whenever the document's content is replaced, e.g. after
// truncation or rotation; reason describes the latest replacement.
func (d *Document) Generation() (int64, string) { return d.data.Generation() }

// WatchStatus describes a persistent watch problem, such as a missing file.
func (d *Document) WatchStatus() string {
	d.watchMu.Lock()
	defer d.watchMu.Unlock()
	return d.watchStatus
}

func (d *Document) setWatchStatus(status string) {
	d.watchMu.Lock()
	defer d.watchMu.Unlock()
	d.watchStatus = status
}

// Watch polls a file-backed document every interval until ctx ends. Appends
// extend the document; a shrinking file reloads from its start. By descriptor,
// it follows the open file (like tail -f). By name, it also reopens path when
// it names a different file, as after rotation, and waits while it is missing
// (like tail -F). Watch never closes file; it closes only files it opened.
func (d *Document) Watch(ctx context.Context, file *os.File, path string, byName bool, interval time.Duration) {
	current := file
	var opened *os.File
	defer func() {
		if opened != nil {
			opened.Close()
		}
	}()
	// known fingerprints the shown content; modified is the file's time when
	// it was last verified, so an idle file costs only a stat per poll.
	known, modified := d.loaded, d.loadedModTime
	load := func(file *os.File, info os.FileInfo, reason string) bool {
		if d.data.reload(file, 0, info.Size(), reason) != nil {
			return false
		}
		known, modified = sample(file, 0, info.Size()), info.ModTime()
		return true
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if byName && path != "" {
			named, err := os.Stat(path)
			if err != nil {
				d.setWatchStatus(fmt.Sprintf("%s: %v; waiting for it to return", path, unwrapPathError(err)))
				continue
			}
			d.setWatchStatus("")
			if info, err := current.Stat(); err != nil || !os.SameFile(info, named) {
				replacement, err := os.Open(path)
				if err != nil {
					d.setWatchStatus(fmt.Sprintf("%s: %v; waiting for it to return", path, unwrapPathError(err)))
					continue
				}
				info, err := replacement.Stat()
				if err != nil || !info.Mode().IsRegular() {
					replacement.Close()
					continue
				}
				if !load(replacement, info, "replaced") {
					replacement.Close()
					return
				}
				if opened != nil {
					opened.Close()
				}
				current, opened = replacement, replacement
				continue
			}
		}
		info, err := current.Stat()
		if err != nil {
			continue
		}
		_, offset, length := d.data.fileState()
		size := info.Size() - offset
		if size == length && info.ModTime().Equal(modified) {
			continue
		}
		if size >= length && bytes.Equal(sample(current, offset, length), known) {
			if size > length {
				d.data.grow(size)
				known = sample(current, offset, size)
			}
			modified = info.ModTime()
			continue
		}
		// The file shrank, or the bytes already shown changed because it was
		// truncated and rewritten between polls. Content before the old offset
		// may also have changed, so show the file from its start.
		if !load(current, info, "truncated") {
			return
		}
	}
}

// sample fingerprints content already shown: its head and the bytes just
// before its end. Appends leave both unchanged.
func sample(file *os.File, offset, length int64) []byte {
	head := make([]byte, min(length, 4096))
	n, _ := file.ReadAt(head, offset)
	tail := make([]byte, min(length, 64))
	m, _ := file.ReadAt(tail, offset+length-int64(len(tail)))
	return append(head[:n], tail[:m]...)
}

func unwrapPathError(err error) error {
	if pathErr, ok := err.(*os.PathError); ok {
		return pathErr.Err
	}
	return err
}
