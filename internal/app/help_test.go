package app

import (
	"strings"
	"testing"

	"glimpse/internal/ui"
)

func TestHelpListsEffectiveBindings(t *testing.T) {
	a, err := New(Options{Keybinds: map[string][]string{"top": {"t"}, "follow": {}, "search-accept": {"<C-s>"}}})
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Join(helpLines(a.Keys, a.SearchKeys), "\n")
	for _, want := range []string{"q  zq  <C-c>", "t  ", "(unbound)", "g?", "<PageDown>  <Space>", "<C-s>", ":set jump-target=N"} {
		if !strings.Contains(text, want) {
			t.Fatalf("help lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "  gg  ") {
		t.Fatalf("replaced default still listed:\n%s", text)
	}
}

func TestRunHelpScreen(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	viewer := startTestViewer(t, Options{CmdHeight: ui.CmdHeightWithSearch}, strings.Repeat("document\n", 40), 60, 8)
	viewer.press("j", "g", "?")
	frame := waitFrame(t, viewer.frames, func(frame ui.Frame) bool { return frame.StatusState.Mode == "HELP" })
	if !strings.HasPrefix(frame.Lines[0], "Glimpse keys") {
		t.Fatalf("help = %q", frame.Lines)
	}
	viewer.press("j")
	waitFrame(t, viewer.frames, firstFrameLine(""))
	viewer.press("G")
	frame = waitFrame(t, viewer.frames, func(frame ui.Frame) bool {
		return strings.HasPrefix(frame.Lines[len(frame.Lines)-1], "A count before G")
	})
	if len(frame.Lines) != 6 {
		t.Fatalf("help bottom = %q", frame.Lines)
	}
	// q closes the help instead of quitting, and the document is unchanged.
	viewer.press("q")
	frame = waitFrame(t, viewer.frames, func(frame ui.Frame) bool { return frame.StatusState.Mode == "VIEW" })
	if frame.StatusState.TopLine != 2 || frame.Lines[0] != "document" {
		t.Fatalf("document after help = %+v", frame.StatusState)
	}
	viewer.press("g", "?")
	waitFrame(t, viewer.frames, func(frame ui.Frame) bool { return frame.StatusState.Mode == "HELP" })
	viewer.press("<Esc>")
	waitFrame(t, viewer.frames, func(frame ui.Frame) bool { return frame.StatusState.Mode == "VIEW" })
	viewer.press("q")
	viewer.expectExit(t)
}
