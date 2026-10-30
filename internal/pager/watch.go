package pager

import (
	"context"
	"fmt"
	"hash/crc32"
	"os"
	"syscall"
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

// RecheckName makes the next poll reopen the watched path if it now names a
// different file, even when following by descriptor. Editors such as vim save
// by renaming a new file over the old one, which a descriptor never sees.
func (d *Document) RecheckName() { d.recheckName.Store(true) }

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
		known, modified = fingerprint(file, 0, info.Size()), info.ModTime()
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
		// A one-time recheck reopens a replaced path but, unlike following by
		// name, ignores a missing path and keeps the descriptor.
		once := !byName && d.recheckName.Swap(false)
		if (byName || once) && path != "" {
			named, err := os.Stat(path)
			if err != nil && byName {
				d.setWatchStatus(fmt.Sprintf("%s: %v; waiting for it to return", path, unwrapPathError(err)))
				continue
			}
			if byName {
				d.setWatchStatus("")
			}
			if info, err := current.Stat(); named != nil && (err != nil || !os.SameFile(info, named)) {
				// Opening a FIFO would block until a writer appears, and
				// cancellation cannot interrupt the open.
				if !named.Mode().IsRegular() {
					if byName {
						d.setWatchStatus(fmt.Sprintf("%s: not a regular file; waiting for it to return", path))
					}
					continue
				}
				// Non-blocking guards against a FIFO renamed in after the stat;
				// it does not affect reads of a regular file.
				replacement, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
				if err != nil {
					if byName {
						d.setWatchStatus(fmt.Sprintf("%s: %v; waiting for it to return", path, unwrapPathError(err)))
					}
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
		// Appends always grow a file, so a changed time without growth means
		// it was written in place; only a small file can be verified unchanged.
		unchanged := size >= length && fingerprint(current, offset, length) == known
		if unchanged && (size > length || length <= exactFingerprint) {
			if size > length {
				d.data.grow(size)
				known = fingerprint(current, offset, size)
			}
			modified = info.ModTime()
			continue
		}
		// The file shrank, or the bytes already shown changed because it was
		// written in place or truncated and rewritten between polls. Content
		// before the old offset may also have changed, so show the file from
		// its start.
		if !load(current, info, "truncated") {
			return
		}
	}
}

// exactFingerprint is the largest content fingerprinted in full; checking it
// takes about a millisecond, and only when the file's time changes.
const exactFingerprint = 1 << 20

var castagnoli = crc32.MakeTable(crc32.Castagnoli)

// fingerprint checksums content already shown, which appends leave unchanged:
// all of it up to exactFingerprint, otherwise its head, its last bytes, and
// evenly spaced probes between them.
func fingerprint(file *os.File, offset, length int64) uint32 {
	if length <= exactFingerprint {
		buf := make([]byte, length)
		n, _ := file.ReadAt(buf, offset)
		return crc32.Checksum(buf[:n], castagnoli)
	}
	const head, probes, probe = 4096, 64, 64
	buf := make([]byte, 0, head+(probes+1)*probe)
	read := func(at, size int64) {
		start := len(buf)
		buf = buf[:start+int(size)]
		n, _ := file.ReadAt(buf[start:], at)
		buf = buf[:start+n]
	}
	read(offset, head)
	for i := int64(1); i <= probes; i++ {
		read(offset+length*i/(probes+1), probe)
	}
	read(offset+length-probe, probe)
	return crc32.Checksum(buf, castagnoli)
}

func unwrapPathError(err error) error {
	if pathErr, ok := err.(*os.PathError); ok {
		return pathErr.Err
	}
	return err
}
