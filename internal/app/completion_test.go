package app

import (
	"reflect"
	"strings"
	"testing"

	"glimpse/internal/keybind"
	"glimpse/internal/ui"
)

func TestCommandCompletions(t *testing.T) {
	for _, tt := range []struct {
		text   string
		cursor int
		start  int
		want   []string
	}{
		{"", 0, 0, commandNames},
		{"q", 1, 0, []string{"q", "quit"}},
		{"se", 2, 0, []string{"set"}},
		{"set h", 5, 4, []string{"hex-colors", "hlsearch"}},
		{"set  j", 6, 5, []string{"jump-target="}},
		{"set now", 7, 4, []string{"nowrap"}},
		{"set n", 5, 4, []string{"nohex-colors", "nohlsearch", "noignorecase", "noincsearch", "nonumber", "nosmartcase", "nowrap", "number"}},
		{"set nu", 6, 4, []string{"number"}},
		{"set noj", 7, 4, nil},
		{"set wrap x", 10, 9, nil},
		{"top w", 5, 4, nil},
		{"zz", 2, 0, nil},
		{"set wrapXYZ", 6, 4, []string{"wrap"}},
	} {
		start, got := commandCompletions([]rune(tt.text), tt.cursor)
		if len(got) == 0 {
			got = nil
		}
		if start != tt.start || !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%q at %d: start %d %q; want %d %q", tt.text, tt.cursor, start, got, tt.start, tt.want)
		}
	}
	if setOptions[0] != "hex-colors" {
		t.Fatal("no- candidates modified the option table")
	}
}

func TestCompletionOptionsAreAccepted(t *testing.T) {
	application, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer application.Document.Close()
	values := map[string]string{"jump-target=": "jump-target=1", "spool-quota=": "spool-quota=1000000000"}
	for _, option := range setOptions {
		input := "set " + option
		if value, ok := values[option]; ok {
			input = "set " + value
		}
		if _, err := application.command(input); err != nil {
			t.Errorf("%s: %v", input, err)
		}
		if !strings.HasSuffix(option, "=") {
			if _, err := application.command("set no" + option); err != nil {
				t.Errorf("set no%s: %v", option, err)
			}
		}
	}
	for _, name := range commandNames {
		if name == "set" {
			continue
		}
		if _, err := application.command(name); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestPromptCompletionCyclesVimStyle(t *testing.T) {
	p := newPrompt(false, nil)
	p.command = true
	p.text, p.cursor = []rune("set h tail"), 5
	step := func(action keybind.Action, want string) {
		t.Helper()
		p.edit(keybind.Command{Action: action})
		if got := string(p.text); got != want || p.cursor != strings.Index(want, " tail") {
			t.Fatalf("prompt = %q at %d; want %q", got, p.cursor, want)
		}
	}
	step(keybind.Complete, "set hex-colors tail")
	step(keybind.Complete, "set hlsearch tail")
	step(keybind.Complete, "set h tail")
	step(keybind.CompletePrevious, "set hlsearch tail")
	// Editing ends the cycle; Tab then completes the edited word afresh.
	p.edit(keybind.Command{Action: keybind.Backspace})
	if p.completion != nil {
		t.Fatal("edit kept the completion")
	}
	step(keybind.Complete, "set hlsearch tail")

	search := newPrompt(false, nil)
	search.text, search.cursor = []rune("se"), 2
	search.edit(keybind.Command{Action: keybind.Complete})
	if string(search.text) != "se" {
		t.Fatalf("search prompt completed: %q", string(search.text))
	}
}

func TestPromptGhost(t *testing.T) {
	for _, tt := range []struct {
		text    string
		cursor  int
		command bool
		want    string
	}{
		{"set hl", 6, true, "search"},
		{"se", 2, true, "t"},
		{"set n", 5, true, "ohex-colors"},
		{"set wrap", 8, true, ""},
		{"", 0, true, ""},
		{"set ", 4, true, ""},
		{"set hl", 5, true, ""},
		{"zz", 2, true, ""},
		{"se", 2, false, ""},
	} {
		p := newPrompt(false, nil)
		p.command, p.text, p.cursor = tt.command, []rune(tt.text), tt.cursor
		if got := p.ghost(); got != tt.want {
			t.Errorf("%q at %d: ghost %q, want %q", tt.text, tt.cursor, got, tt.want)
		}
	}
}

func TestRunCommandCompletion(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	viewer := startTestViewer(t, Options{CmdHeight: ui.CmdHeightToggle}, "a very long line here\n", 20, 4)
	viewer.press(":")
	viewer.typeText("se")
	waitFrame(t, viewer.frames, func(frame ui.Frame) bool { return frame.Search == ":se" && frame.Ghost == "t" })
	viewer.press("<Tab>")
	viewer.typeText(" now")
	viewer.press("<Tab>")
	waitFrame(t, viewer.frames, func(frame ui.Frame) bool { return frame.Search == ":set nowrap" && frame.Ghost == "" })
	viewer.press("<Enter>")
	waitFrame(t, viewer.frames, func(frame ui.Frame) bool { return frame.Search == "nowrap" })
	viewer.press(":")
	viewer.typeText("q")
	viewer.press("<S-Tab>")
	waitFrame(t, viewer.frames, func(frame ui.Frame) bool { return frame.Search == ":quit" })
	viewer.press("<Enter>")
	viewer.expectExit(t)
}
