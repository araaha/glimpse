package search

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestHistoryPaths(t *testing.T) {
	home := t.TempDir()
	data := t.TempDir()
	t.Setenv("HOME", home)
	for _, tt := range []struct{ xdg, want string }{
		{data, filepath.Join(data, "glimpse", "search-history")},
		{"", filepath.Join(home, ".local", "share", "glimpse", "search-history")},
		{"relative", filepath.Join(home, ".local", "share", "glimpse", "search-history")},
	} {
		t.Setenv("XDG_DATA_HOME", tt.xdg)
		got, err := DefaultHistoryPath()
		if got != tt.want || err != nil {
			t.Fatalf("path = (%q, %v), want %q", got, err, tt.want)
		}
	}
}

func TestHistoryPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "glimpse", "search-history")
	h := History{Path: path}
	if err := h.Load(); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{"one", "界 \"quoted\"\nquery", "two", "one"} {
		if err := h.Add(query); err != nil {
			t.Fatal(err)
		}
	}
	// Another running viewer must append without replacing the first's entries.
	other := History{Path: path}
	if err := other.Add("other"); err != nil {
		t.Fatal(err)
	}
	reloaded := History{Path: path}
	if err := reloaded.Load(); err != nil {
		t.Fatal(err)
	}
	want := []string{"界 \"quoted\"\nquery", "two", "one", "other"}
	if !reflect.DeepEqual(reloaded.Entries, want) {
		t.Fatalf("entries = %q, want %q", reloaded.Entries, want)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("history permissions: %v, %v", info, err)
	}
	info, err = os.Stat(filepath.Dir(path))
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("directory permissions: %v, %v", info, err)
	}
}

func TestHistoryMalformedAndLimits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history")
	if err := os.WriteFile(path, []byte("not-json\n\"ok\"\n\"\"\n\"unfinished"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := History{Path: path}
	if err := h.Load(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(h.Entries, []string{"ok"}) {
		t.Fatalf("entries = %q", h.Entries)
	}
	if err := h.Add("after interruption"); err != nil {
		t.Fatal(err)
	}
	reloaded := History{Path: path}
	if err := reloaded.Load(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(reloaded.Entries, []string{"ok", "after interruption"}) {
		t.Fatalf("truncated record corrupted append: %q", reloaded.Entries)
	}
	if err := h.Add(strings.Repeat("x", MaxQueryBytes+1)); err != nil {
		t.Fatal(err)
	}
	if len(h.Entries) != 2 {
		t.Fatal("oversized query remembered")
	}
	for i := 0; i < maxHistoryEntries+10; i++ {
		h.remember(strings.Repeat("x", i+1))
	}
	if len(h.Entries) != maxHistoryEntries {
		t.Fatalf("retained %d history entries", len(h.Entries))
	}
}
