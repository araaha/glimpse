package app

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"glimpse/internal/ui"
)

func prefixedLines(prefix string, n int) string {
	var text strings.Builder
	for line := 1; line <= n; line++ {
		fmt.Fprintf(&text, "%s%d\n", prefix, line)
	}
	return text.String()
}

func TestRunReloadKeepsTheLine(t *testing.T) {
	path, file := numberedFile(t, "lines.txt", 60)
	viewer := startReaderViewer(t, Options{CmdHeight: ui.CmdHeightWithSearch, JumpTarget: JumpTarget{Fraction: .5}}, file, path, 20, 8, false)
	viewer.press(":", "3", "0", "<Enter>")
	waitFrame(t, viewer.frames, func(frame ui.Frame) bool { return len(frame.Lines) > 3 && frame.Lines[3] == "30" })
	// Rewriting in place reloads the file; line 30 stays on the jump-target row.
	if err := os.WriteFile(path, []byte(prefixedLines("a", 60)), 0o600); err != nil {
		t.Fatal(err)
	}
	waitFrame(t, viewer.frames, func(frame ui.Frame) bool {
		return len(frame.Lines) > 3 && frame.Lines[3] == "a30" && strings.HasSuffix(frame.Search, "; reloaded")
	})
	// A file now shorter than the line shows its end.
	if err := os.WriteFile(path, []byte(prefixedLines("b", 10)), 0o600); err != nil {
		t.Fatal(err)
	}
	waitFrame(t, viewer.frames, func(frame ui.Frame) bool { return slices.Contains(frame.Lines, "b10") })
	viewer.press("q")
	viewer.expectExit(t)
}

func TestRunCommandShowsFileSavedByRename(t *testing.T) {
	path, file := numberedFile(t, "notes.txt", 60)
	// Save like vim: write a new file, then rename it over the old one.
	save := `for i in $(seq 1 60); do echo "x$i"; done > "$file.new" && mv "$file.new" "$file"`
	viewer := startReaderViewer(t, Options{CmdHeight: ui.CmdHeightWithSearch, Run: []RunCommand{{Keys: []string{"S"}, Command: save}}}, file, path, 20, 8, false)
	viewer.press(":", "4", "2", "<Enter>")
	waitFrame(t, viewer.frames, func(frame ui.Frame) bool { return len(frame.Lines) > 0 && frame.Lines[0] == "42" })
	viewer.press("S")
	waitFrame(t, viewer.frames, func(frame ui.Frame) bool {
		return len(frame.Lines) > 0 && frame.Lines[0] == "x42" && strings.HasSuffix(frame.Search, "/notes.txt replaced; reloaded")
	})
	viewer.press("q")
	viewer.expectExit(t)
}

func TestRunFollowedReloadStaysAtTheEnd(t *testing.T) {
	path, file := numberedFile(t, "app.log", 60)
	viewer := startReaderViewer(t, Options{CmdHeight: ui.CmdHeightWithSearch, Follow: true}, file, path, 20, 8, false)
	if err := os.WriteFile(path, []byte(prefixedLines("c", 40)), 0o600); err != nil {
		t.Fatal(err)
	}
	waitFrame(t, viewer.frames, func(frame ui.Frame) bool { return len(frame.Lines) > 5 && frame.Lines[5] == "c40" })
	viewer.press("q")
	viewer.expectExit(t)
}
