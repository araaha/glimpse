package app

import (
	"testing"

	"glimpse/internal/ui"
)

func TestRunHexColorsToggle(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("COLORTERM", "truecolor")
	viewer := startTestViewer(t, Options{CmdHeight: ui.CmdHeightWithSearch, HexColors: true}, "bg #ff8800\n", 20, 4)
	viewer.press("=") // Repaint; startup already consumed the settled frame.
	frame := waitFrame(t, viewer.frames, func(frame ui.Frame) bool { return len(frame.HexColors) == 7 })
	if code := frame.HexColors[0]; code != (ui.HexColor{Column: 3, Width: 1, RGB: 0xff8800}) {
		t.Fatalf("first cell = %+v", code)
	}
	viewer.press("R")
	waitFrame(t, viewer.frames, func(frame ui.Frame) bool { return frame.HexColors == nil })
	viewer.press(":", "s", "e", "t", " ", "h", "e", "x", "-", "c", "o", "l", "o", "r", "s", "<Enter>")
	waitFrame(t, viewer.frames, func(frame ui.Frame) bool { return len(frame.HexColors) == 7 && frame.Search == "hex-colors=true" })
	viewer.press("q")
	viewer.expectExit(t)
}

func TestRunHexColorsNeedTruecolor(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("COLORTERM", "")
	viewer := startTestViewer(t, Options{CmdHeight: ui.CmdHeightWithSearch}, "bg #ff8800\n", 20, 4)
	viewer.press("R")
	frame := waitFrame(t, viewer.frames, func(frame ui.Frame) bool { return frame.Search == hexColorsUnsupported })
	if frame.HexColors != nil {
		t.Fatalf("colored without truecolor: %+v", frame.HexColors)
	}
	viewer.press("q")
	viewer.expectExit(t)
}
