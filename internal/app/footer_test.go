package app

import (
	"fmt"
	"strings"
	"testing"

	"github.com/araaha/glimpse/internal/ui"
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

// A completed search echoes its query below status in modes 0 and 1. Mode -1
// shares one row, so it returns to the statusline, which reports the match.
// No mode shows a transient "Searching" message.
func TestRunSearchFooterAfterAccept(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	for _, mode := range []ui.CmdHeight{0, 1, -1} {
		t.Run(fmt.Sprint(mode), func(t *testing.T) {
			viewer := startTestViewer(t, Options{CmdHeight: mode}, "one\ntwo\nthree\nfour\nfive\nsix\nseven\nfourB\nnine\nten\neleven", 20, 4)
			viewer.press("/")
			viewer.typeText("four")
			viewer.press("<Enter>")
			frame := waitFrame(t, viewer.frames, func(frame ui.Frame) bool {
				if strings.Contains(frame.Search, "Searching") {
					t.Fatalf("transient search message: %q", frame.Search)
				}
				return firstFrameLine("four")(frame) && frame.StatusState.SearchTotalKnown
			})
			if mode == -1 {
				if frame.Search != "" || frame.Layout.StatusRow < 0 || frame.Layout.SearchRow >= 0 || frame.StatusState.SearchCurrent != 1 {
					t.Fatalf("mode -1 footer = %+v, search %q, status %+v", frame.Layout, frame.Search, frame.StatusState)
				}
			} else if frame.Search != "/four" || frame.Layout.StatusRow < 0 || frame.Layout.SearchRow < 0 {
				t.Fatalf("mode %d footer = %+v, search %q", mode, frame.Layout, frame.Search)
			}
			viewer.press("n")
			frame = waitFrame(t, viewer.frames, func(frame ui.Frame) bool {
				if strings.Contains(frame.Search, "Searching") || mode == -1 && frame.Search != "" {
					t.Fatalf("repeat footer: %q", frame.Search)
				}
				return firstFrameLine("fourB")(frame) && frame.StatusState.SearchCurrentKnown
			})
			if frame.StatusState.SearchCurrent != 2 || mode != -1 && frame.Search != "/four" {
				t.Fatalf("repeat = %q, status %+v", frame.Search, frame.StatusState)
			}
			viewer.press("q")
			viewer.expectExit(t)
		})
	}
}
