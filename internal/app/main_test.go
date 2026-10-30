package app

import (
	"os"
	"testing"
)

// TestMain keeps spilled indexes and spools, which default to
// $XDG_CACHE_HOME/glimpse, out of the user's cache directory.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "glimpse-test-cache-")
	if err != nil {
		panic(err)
	}
	os.Setenv("XDG_CACHE_HOME", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
