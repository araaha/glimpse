package app

import (
	"fmt"
	"testing"

	"glimpse/internal/ui"
)

func TestRunCmdheightModes(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	for _, mode := range []ui.CmdHeight{0, 1, -1} {
		t.Run(fmt.Sprint(mode), func(t *testing.T) {
			viewer := startTestViewer(t, Options{CmdHeight: mode}, "one\ntwo\nthree\nfour\nfive", 20, 5)
			check := func(status, search, height int, active bool) {
				t.Helper()
				frame := waitFrame(t, viewer.frames, func(ui.Frame) bool { return true })
				if frame.Layout.StatusRow != status || frame.Layout.SearchRow != search || frame.Layout.Body.Height != height || frame.SearchActive != active {
					t.Fatalf("footer = %+v, active %v; want status %d search %d height %d active %v", frame.Layout, frame.SearchActive, status, search, height, active)
				}
				if !firstFrameLine("one")(frame) {
					t.Fatalf("footer change moved top row: %q", frame.Lines)
				}
			}
			status, search, height := 4, -1, 4
			if mode == 1 {
				status, search, height = 3, 4, 3
			}
			viewer.press("=")
			if mode == -1 {
				check(-1, -1, 5, false)
			} else {
				check(status, search, height, false) // = cannot hide fixed rows.
			}
			for _, prefix := range []string{"/", "?", ":"} {
				viewer.press(prefix)
				promptStatus, promptHeight := -1, 4
				if mode != -1 {
					promptStatus, promptHeight = 3, 3
				}
				check(promptStatus, 4, promptHeight, true)
				viewer.press("<Esc>")
				if mode == -1 {
					check(-1, -1, 5, false) // Prompt does not unhide the statusline.
				} else {
					check(status, search, height, false)
				}
			}
			viewer.press("=")
			check(status, search, height, false)
			viewer.press("q")
			viewer.expectExit(t)
		})
	}
}
