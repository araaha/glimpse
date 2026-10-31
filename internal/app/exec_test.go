package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/araaha/glimpse/internal/ui"
)

// waitFile waits for a command's output file to hold want.
func waitFile(t *testing.T, path, want string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		data, err := os.ReadFile(path)
		if err == nil && string(data) == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s = %q (%v), want %q", filepath.Base(path), data, err, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// numberedFile writes lines "1".."n" to a file whose name may need quoting.
func numberedFile(t *testing.T, name string, n int) (string, *os.File) {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	var content strings.Builder
	for line := 1; line <= n; line++ {
		fmt.Fprintf(&content, "%d\n", line)
	}
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return path, file
}

func TestRunCommandLineFollowsJumpTarget(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out")
	run := []RunCommand{{Keys: []string{"s", "h"}, Command: `printf '%s %s' "$line" "$file" > ` + out}}
	for _, tt := range []struct {
		name      string
		target    JumpTarget
		keys      []string
		want, row int // the 6-row body shows want on row once the view settles
	}{
		{"top by default", JumpTarget{}, nil, 1, 0},
		{"scrolled", JumpTarget{}, []string{"j", "j"}, 3, 0},
		{":N jump at the top", JumpTarget{}, []string{":", "2", "0", "<Enter>"}, 20, 0},
		{":N jump at the middle", JumpTarget{Fraction: .5}, []string{":", "2", "0", "<Enter>"}, 20, 3},
		{"search at the bottom", JumpTarget{Line: -1}, []string{"/", "3", "3", "<Enter>"}, 33, 5},
		{"search at the top", JumpTarget{}, []string{"/", "3", "3", "<Enter>"}, 33, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			os.Remove(out)
			path, file := numberedFile(t, "lines.txt", 60)
			viewer := startReaderViewer(t, Options{CmdHeight: ui.CmdHeightWithSearch, JumpTarget: tt.target, Run: run}, file, path, 20, 8, false)
			if len(tt.keys) == 0 {
				// Repaint; startup already consumed the settled frame. Other keys
				// repaint by themselves (and = would cancel a running search).
				tt.keys = []string{"=", "="}
			}
			viewer.press(tt.keys...)
			waitFrame(t, viewer.frames, func(frame ui.Frame) bool {
				return len(frame.Lines) > tt.row && frame.Lines[tt.row] == fmt.Sprint(tt.want)
			})
			viewer.press("s", "h")
			waitFile(t, out, fmt.Sprintf("%d %s", tt.want, path))
			viewer.press("q")
			viewer.expectExit(t)
		})
	}
}

func TestRunCommandFileIsNeverShellSyntax(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir) // Where an injected touch would land.
	out := filepath.Join(dir, "out")
	path, file := numberedFile(t, "a $(touch PWNED) `touch PWNED` b;.txt", 3)
	viewer := startReaderViewer(t, Options{Run: []RunCommand{{Keys: []string{"S"}, Command: `printf %s "$file" > ` + out}}}, file, path, 20, 4, false)
	viewer.press("S")
	waitFile(t, out, path)
	if _, err := os.Stat(filepath.Join(dir, "PWNED")); err == nil {
		t.Fatal("file name was executed")
	}
	viewer.press("q")
	viewer.expectExit(t)
}

func TestRunCommandsBackgroundAndFailures(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out")
	path, file := numberedFile(t, "lines.txt", 5)
	viewer := startReaderViewer(t, Options{CmdHeight: ui.CmdHeightWithSearch, Run: []RunCommand{
		{Keys: []string{"s", "b"}, Command: `sleep .1; printf %s "$line" > ` + out, Background: true},
		{Keys: []string{"s", "f"}, Command: "exit 3"},
		{Keys: []string{"<C-a>"}, Command: "exit 4", Background: true},
	}}, file, path, 20, 4, false)
	viewer.press("s", "b")
	waitFile(t, out, "1")
	viewer.press("s", "f")
	waitFrame(t, viewer.frames, func(frame ui.Frame) bool { return frame.Search == "sf: exited with status 3" })
	viewer.press("<C-a>")
	waitFrame(t, viewer.frames, func(frame ui.Frame) bool { return frame.Search == "<C-a>: exited with status 4" })
	viewer.press("q")
	viewer.expectExit(t)
}

func TestRunEditOpensVisualAtLine(t *testing.T) {
	dir := t.TempDir()
	out, stub := filepath.Join(dir, "out"), filepath.Join(dir, "editor")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nprintf '%s|' \"$@\" > "+out+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VISUAL", stub)
	path, file := numberedFile(t, "my notes.txt", 30)
	viewer := startReaderViewer(t, Options{}, file, path, 20, 4, false)
	viewer.press(":", "7", "<Enter>")
	waitFrame(t, viewer.frames, func(frame ui.Frame) bool { return len(frame.Lines) > 0 && frame.Lines[0] == "7" })
	viewer.press("v")
	waitFile(t, out, "+7|"+path+"|")
	viewer.press("q")
	viewer.expectExit(t)

	stdin := startNamedTestViewer(t, Options{CmdHeight: ui.CmdHeightWithSearch}, "piped\n", "", 20, 4)
	stdin.press("v")
	waitFrame(t, stdin.frames, func(frame ui.Frame) bool { return frame.Search == "No file to edit" })
	stdin.press("q")
	stdin.expectExit(t)
}

func TestRunBindingsValidation(t *testing.T) {
	for _, tt := range []struct {
		runs []RunCommand
		want string
	}{
		{[]RunCommand{{Command: "true"}}, "run-1: keys must not be empty"},
		{[]RunCommand{{Keys: []string{"S"}, Command: " "}}, "run-1: command must not be empty"},
		{[]RunCommand{{Keys: []string{"S"}, Command: "true"}, {Keys: []string{"<bad"}, Command: "true"}}, "run-2: invalid key"},
		{[]RunCommand{{Keys: []string{"v"}, Command: "true"}}, "bound to both edit and run-1"},
		{[]RunCommand{{Keys: []string{"S"}, Command: "a"}, {Keys: []string{"S"}, Command: "b"}}, "bound to both run-1 and run-2"},
	} {
		if _, err := New(Options{Run: tt.runs}); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%+v: error %v, want %q", tt.runs, err, tt.want)
		}
	}
	a, err := New(Options{Keybinds: map[string][]string{"edit": {}}, Run: []RunCommand{{Keys: []string{"v"}, Command: "true"}}})
	if err != nil {
		t.Fatalf("v freed by disabling edit: %v", err)
	}
	a.Document.Close()
}
