package app

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/araaha/glimpse/internal/ui"
)

func numberedLines(count int, mark func(int) bool) string {
	var input strings.Builder
	for line := 1; line <= count; line++ {
		if mark(line) {
			fmt.Fprintf(&input, "line%02d hit\n", line)
		} else {
			fmt.Fprintf(&input, "line%02d\n", line)
		}
	}
	return input.String()
}

func linesAre(want ...string) func(ui.Frame) bool {
	return func(frame ui.Frame) bool { return slices.Equal(frame.Lines, want) }
}

func TestRunFilterShowsMatchingLinesWithTheirNumbers(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	input := numberedLines(30, func(line int) bool { return line%5 == 0 })
	viewer := startTestViewer(t, Options{CmdHeight: ui.CmdHeightWithSearch, LineNumbers: true}, input, 20, 6)
	viewer.press("&")
	viewer.typeText("hit")
	viewer.press("<Enter>")
	frame := waitFrame(t, viewer.frames, linesAre("line05 hit", "line10 hit", "line15 hit", "line20 hit"))
	if want := []string{"5", "10", "15", "20"}; !slices.Equal(frame.Gutter, want) {
		t.Fatalf("gutter = %q, want %q", frame.Gutter, want)
	}
	if frame.StatusState.Mode != ui.ModeFilter || frame.StatusState.TopLine != 5 {
		t.Fatalf("status = %+v", frame.StatusState)
	}
	if frame.Search != "&hit" {
		t.Fatalf("message = %q", frame.Search)
	}
	// :N goes to the first matching line at or after line N.
	viewer.press(":")
	viewer.typeText("12")
	viewer.press("<Enter>")
	frame = waitFrame(t, viewer.frames, firstFrameLine("line15 hit"))
	if frame.StatusState.TopLine != 15 {
		t.Fatalf("top line = %d", frame.StatusState.TopLine)
	}
	// Searches stay within the matching lines.
	viewer.press("/")
	viewer.typeText("line2")
	viewer.press("<Enter>")
	waitFrame(t, viewer.frames, firstFrameLine("line20 hit"))
	// An empty filter shows every line again, with the same line on top.
	viewer.press("&", "<Enter>")
	frame = waitFrame(t, viewer.frames, func(frame ui.Frame) bool {
		return frame.StatusState.Mode == ui.ModeView && len(frame.Lines) > 1 && frame.Lines[0] == "line20 hit" && frame.Lines[1] == "line21"
	})
	if frame.Gutter[0] != "20" {
		t.Fatalf("gutter = %q", frame.Gutter)
	}
}

func TestRunFilterCommandsAndMisses(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	input := numberedLines(30, func(line int) bool { return line == 3 || line == 4 })
	viewer := startTestViewer(t, Options{CmdHeight: ui.CmdHeightWithSearch}, input, 20, 6)
	viewer.press(":")
	viewer.typeText("filter line0[34]")
	viewer.press("<Enter>")
	waitFrame(t, viewer.frames, linesAre("line03 hit", "line04 hit"))
	viewer.press(":")
	viewer.typeText("filter absent")
	viewer.press("<Enter>")
	waitFrame(t, viewer.frames, func(frame ui.Frame) bool {
		return len(frame.Lines) == 0 && frame.Search == "Pattern not found: &absent"
	})
	viewer.press(":")
	// The empty filter kept the line it was set at, line 3, for :nofilter.
	viewer.typeText("nofilter")
	viewer.press("<Enter>")
	waitFrame(t, viewer.frames, func(frame ui.Frame) bool {
		return len(frame.Lines) == 4 && frame.Lines[0] == "line03 hit" && frame.Lines[2] == "line05" && frame.StatusState.Mode == ui.ModeView
	})
}

// A filtered file that is followed shows matching lines as they are appended.
func TestRunFilterFollowsAppends(t *testing.T) {
	path, file := openFollowed(t, "keep 1\ndrop\n")
	viewer := startReaderViewer(t, Options{CmdHeight: ui.CmdHeightWithSearch, Follow: true}, file, path, 20, 5, false)
	viewer.press("&")
	viewer.typeText("keep")
	viewer.press("<Enter>")
	waitFrame(t, viewer.frames, linesAre("keep 1"))
	writeLog(t, path, "drop\nkeep 2\ndrop\nkeep 3\nkeep 4\nkee", os.O_APPEND)
	waitFrame(t, viewer.frames, linesAre("keep 2", "keep 3", "keep 4"))
	writeLog(t, path, "p 5\n", os.O_APPEND)
	frame := waitFrame(t, viewer.frames, linesAre("keep 3", "keep 4", "keep 5"))
	if frame.StatusState.Mode != ui.ModeFollow {
		t.Fatalf("mode = %s", frame.StatusState.Mode)
	}
}

// A reload refilters the new content.
func TestRunFilterSurvivesReload(t *testing.T) {
	path, file := openFollowed(t, "keep 1\ndrop\nkeep 2\n")
	viewer := startReaderViewer(t, Options{CmdHeight: ui.CmdHeightWithSearch}, file, path, 20, 5, false)
	viewer.press("&")
	viewer.typeText("keep")
	viewer.press("<Enter>")
	waitFrame(t, viewer.frames, linesAre("keep 1", "keep 2"))
	writeLog(t, path, "drop\nkeep 3\n", os.O_TRUNC)
	frame := waitFrame(t, viewer.frames, linesAre("keep 3"))
	if frame.StatusState.Mode != ui.ModeFilter || frame.StatusState.TopLine != 2 {
		t.Fatalf("status = %+v", frame.StatusState)
	}
}

// G goes to the last matching line, even before the filter has found it, and
// when no matching line follows it to make the view grow.
func TestRunFilterBottom(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	input := numberedLines(400000, func(line int) bool { return line%100000 == 1 })
	viewer := startTestViewer(t, Options{CmdHeight: ui.CmdHeightWithSearch, LineNumbers: true}, input, 30, 6)
	viewer.press("&")
	viewer.typeText("hit")
	viewer.press("<Enter>", "G")
	waitFrame(t, viewer.frames, func(frame ui.Frame) bool {
		return len(frame.Lines) == 4 && frame.Lines[3] == "line300001 hit" && frame.Gutter[3] == "300001"
	})
}
