package pager

import (
	"errors"
	"os"
	"path/filepath"
)

// TempFile is a private temporary file. It is unlinked as soon as it is
// created, so it never outlives glimpse, even when glimpse is killed. If
// unlinking fails, Close removes it instead.
type TempFile struct {
	*os.File
	linked bool
}

// CreateTemp creates a TempFile in dir, or in TempDir when dir is empty.
func CreateTemp(dir, pattern string) (*TempFile, error) {
	if dir == "" {
		dir = TempDir()
	}
	file, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return nil, err
	}
	return &TempFile{File: file, linked: os.Remove(file.Name()) != nil}, nil
}

func (f *TempFile) Close() error {
	err := f.File.Close()
	if f.linked {
		err = errors.Join(err, os.Remove(f.Name()))
	}
	return err
}

// TempDir is $XDG_CACHE_HOME/glimpse (by default ~/.cache/glimpse), created on
// demand, because the system temporary directory is often a RAM-backed tmpfs.
// When the cache directory is unavailable, it is the system temporary directory.
func TempDir() string {
	if cache, err := os.UserCacheDir(); err == nil {
		dir := filepath.Join(cache, "glimpse")
		if os.MkdirAll(dir, 0o700) == nil {
			return dir
		}
	}
	return os.TempDir()
}
