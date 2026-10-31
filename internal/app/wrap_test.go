package app

import (
	"fmt"
	"strings"
	"testing"

	"github.com/araaha/glimpse/internal/ui"
)

func TestRunWrapToggleAndHorizontalScroll(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	var input strings.Builder
	for line := 1; line <= 20; line++ {
		fmt.Fprintf(&input, "L%02d-%s-tail%02d\n", line, strings.Repeat("x", 20), line)
	}
	// Each line is 31 columns: three wrapped rows at width 12.
	viewer := startTestViewer(t, Options{CmdHeight: ui.CmdHeightWithSearch}, input.String(), 12, 6)
	viewer.press("5", "G")
	waitFrame(t, viewer.frames, firstFrameLine("L05-xxxxxxxx"))
	viewer.press("w")
	frame := waitFrame(t, viewer.frames, func(frame ui.Frame) bool {
		return len(frame.Lines) > 1 && frame.Lines[0] == "L05-xxxxxxxx" && frame.Lines[1] == "L06-xxxxxxxx"
	})
	if frame.StatusState.TopLine != 5 {
		t.Fatalf("toggle lost position: %+v", frame.StatusState)
	}
	viewer.press("l")
	waitFrame(t, viewer.frames, firstFrameLine("xxxxxxxxxxxx"))
	viewer.press("l", "l")
	waitFrame(t, viewer.frames, firstFrameLine("xxxxxx-tail0"))
	viewer.press("l")
	waitFrame(t, viewer.frames, firstFrameLine("-tail05"))
	// Nothing continues past the edge, so scrolling right stops.
	viewer.press("l", "<Left>")
	waitFrame(t, viewer.frames, firstFrameLine("xxxxxx-tail0"))
	viewer.press("h", "h", "h")
	waitFrame(t, viewer.frames, firstFrameLine("L05-xxxxxxxx"))
	// A match beyond the right edge scrolls into view.
	viewer.press("/")
	viewer.typeText("tail09")
	viewer.press("<Enter>")
	frame = waitFrame(t, viewer.frames, func(frame ui.Frame) bool {
		return frame.StatusState.TopLine == 9 && strings.Contains(frame.Lines[0], "tail09")
	})
	if len(frame.Highlights) == 0 {
		t.Fatal("revealed match is not highlighted")
	}
	viewer.press("w")
	waitFrame(t, viewer.frames, firstFrameLine("L09-xxxxxxxx"))
	viewer.press("q")
	viewer.expectExit(t)
}
