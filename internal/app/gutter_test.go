package app

import (
	"fmt"
	"strings"
	"testing"

	"glimpse/internal/ui"
)

func TestRunLineNumberGutter(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	var input strings.Builder
	input.WriteString("abcdefghijkl\n")
	for line := 2; line <= 1200; line++ {
		fmt.Fprintf(&input, "%d\n", line)
	}
	viewer := startTestViewer(t, Options{CmdHeight: ui.CmdHeightWithSearch, LineNumbers: true}, input.String(), 13, 6)
	viewer.press("=") // Repaint; startup already consumed the settled frame.
	// 1200 lines need four digits plus a space, leaving 8 text columns.
	frame := waitFrame(t, viewer.frames, func(frame ui.Frame) bool {
		return frame.GutterWidth == 5 && len(frame.Lines) == 4 && frame.Lines[0] == "abcdefgh"
	})
	if want := []string{"1", "", "2", "3"}; fmt.Sprint(frame.Gutter) != fmt.Sprint(want) {
		t.Fatalf("gutter = %q", frame.Gutter)
	}
	viewer.press("w")
	frame = waitFrame(t, viewer.frames, func(frame ui.Frame) bool { return len(frame.Gutter) > 1 && frame.Gutter[1] == "2" })
	if frame.Lines[0] != "abcdefgh" || frame.GutterWidth != 5 {
		t.Fatalf("unwrapped gutter frame = %+v", frame)
	}
	viewer.press("#")
	frame = waitFrame(t, viewer.frames, func(frame ui.Frame) bool { return frame.GutterWidth == 0 })
	if frame.Lines[0] != "abcdefghijkl" || frame.Gutter != nil {
		t.Fatalf("gutter not removed: %+v", frame)
	}
	viewer.press("q")
	viewer.expectExit(t)
}
