package app

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/araaha/glimpse/internal/ui"
)

func TestRunMarksAndPreviousPosition(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	input := numberedLines(60, func(line int) bool { return line%10 == 0 })
	viewer := startTestViewer(t, Options{CmdHeight: ui.CmdHeightWithSearch}, input, 20, 6)
	viewer.press("1", "2", "G")
	waitFrame(t, viewer.frames, firstFrameLine("line12"))
	viewer.press("m", "a", "G")
	waitFrame(t, viewer.frames, firstFrameLine("line57"))
	viewer.press("'", "a")
	waitFrame(t, viewer.frames, firstFrameLine("line12"))
	// '' returns to where the last jump started, back and forth.
	viewer.press("'", "'")
	waitFrame(t, viewer.frames, firstFrameLine("line57"))
	viewer.press("'", "'")
	waitFrame(t, viewer.frames, firstFrameLine("line12"))
	// A search is a jump too.
	viewer.press("/")
	viewer.typeText("line40")
	viewer.press("<Enter>")
	waitFrame(t, viewer.frames, firstFrameLine("line40 hit"))
	viewer.press("'", "'")
	waitFrame(t, viewer.frames, firstFrameLine("line12"))
	viewer.press("'", "b")
	waitFrame(t, viewer.frames, searchFrame("Mark b is not set", false))
	viewer.press("m", "B")
	waitFrame(t, viewer.frames, searchFrame("Marks are named a to z", false))
}

// Marks are document positions, so they hold across a filter. A hidden mark
// goes to the next matching line.
func TestRunMarksWithFilter(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	input := numberedLines(60, func(line int) bool { return line%10 == 0 })
	viewer := startTestViewer(t, Options{CmdHeight: ui.CmdHeightWithSearch}, input, 30, 6)
	viewer.press("2", "0", "G", "m", "a", "2", "5", "G", "m", "b", "g", "g")
	waitFrame(t, viewer.frames, firstFrameLine("line01"))
	viewer.press("&")
	viewer.typeText("hit")
	viewer.press("<Enter>")
	waitFrame(t, viewer.frames, firstFrameLine("line10 hit"))
	viewer.press("'", "a")
	waitFrame(t, viewer.frames, firstFrameLine("line20 hit"))
	viewer.press("'", "b")
	waitFrame(t, viewer.frames, func(frame ui.Frame) bool {
		return len(frame.Lines) > 0 && frame.Lines[0] == "line30 hit" &&
			frame.Search == "Line 25 is filtered out; showing the next matching line"
	})
}

func TestRunMarksCommands(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	input := numberedLines(60, func(line int) bool { return false })
	viewer := startTestViewer(t, Options{CmdHeight: ui.CmdHeightWithSearch}, input, 30, 8)
	viewer.press("7", "G", "m", "c", "3", "G", "m", "a")
	viewer.press(":")
	viewer.typeText("marks")
	viewer.press("<Enter>")
	frame := waitFrame(t, viewer.frames, func(frame ui.Frame) bool { return frame.StatusState.Mode == ui.ModeHelp })
	want := []string{" a         3  line03", " c         7  line07", " '         7  line07"}
	if !slices.Equal(frame.Lines[3:6], want) {
		t.Fatalf("marks = %q, want %q", frame.Lines, want)
	}
	viewer.press("q", ":")
	viewer.typeText("delmarks a")
	viewer.press("<Enter>", "'", "a")
	waitFrame(t, viewer.frames, searchFrame("Mark a is not set", false))
	viewer.press(":")
	viewer.typeText("delmarks!")
	viewer.press("<Enter>", "'", "c")
	waitFrame(t, viewer.frames, searchFrame("Mark c is not set", false))
}

// After a reload replaces the content, a mark goes to its line number.
func TestRunMarksAfterReload(t *testing.T) {
	path, file := openFollowed(t, numberedLines(30, func(int) bool { return false }))
	viewer := startReaderViewer(t, Options{CmdHeight: ui.CmdHeightWithSearch}, file, path, 20, 5, false)
	viewer.press("2", "0", "G", "m", "a", "g", "g")
	waitFrame(t, viewer.frames, firstFrameLine("line01"))
	writeLog(t, path, strings.ReplaceAll(numberedLines(30, func(int) bool { return false }), "line", "new"), os.O_TRUNC)
	waitFrame(t, viewer.frames, func(frame ui.Frame) bool { return strings.HasSuffix(frame.Search, "reloaded") })
	viewer.press("'", "a")
	waitFrame(t, viewer.frames, firstFrameLine("new20"))
}
