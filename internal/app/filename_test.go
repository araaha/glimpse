package app

import (
	"os"
	"path/filepath"
	"testing"

	"glimpse/internal/ui"
)

func TestRunStatuslineFilenameStyle(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	path := filepath.Join(home, ".config", "neovim", "init.lua")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		style, label, want string
		path               bool
	}{
		{"", "", "~/.config/neovim/init.lua", true},
		{ui.FilenameShort, "", "~/.c/n/init.lua", true},
		{ui.FilenameTail, "", "init.lua", true},
		{ui.FilenameShort, "my/label", "my/label", false},
	} {
		file, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		options := Options{Label: tt.label, Statusline: ui.StatuslineConfig{FilenameStyle: tt.style}}
		viewer := startReaderViewer(t, options, file, path, 40, 3, false)
		viewer.press("=")
		frame := waitFrame(t, viewer.frames, func(frame ui.Frame) bool { return frame.StatusState.Filename != "" })
		if frame.StatusState.Filename != tt.want || frame.StatusState.FilenamePath != tt.path {
			t.Errorf("style %q label %q: %q (path %v), want %q", tt.style, tt.label, frame.StatusState.Filename, frame.StatusState.FilenamePath, tt.want)
		}
		viewer.press("q")
		viewer.expectExit(t)
	}
}
