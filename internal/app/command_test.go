package app

import (
	"github.com/araaha/glimpse/internal/keybind"
	"testing"
)

func TestCommands(t *testing.T) {
	a, _ := New(Options{})
	for _, tt := range []struct {
		input  string
		quit   bool
		action keybind.Action
		line   int
	}{
		{"q", true, "", 0}, {"quit", true, "", 0}, {"top", false, keybind.Top, 0},
		{"bottom", false, keybind.Bottom, 0}, {"50", false, "", 50}, {"", false, "", 0},
	} {
		got, err := a.command(tt.input)
		if err != nil || got.quit != tt.quit || got.action != tt.action || got.line != tt.line {
			t.Fatalf("%q: %+v, %v", tt.input, got, err)
		}
	}
	for _, input := range []string{"!rm", "0", "-1", "set unknown", "set smartcase=true", "set nojump-target", "set no", "set jump-target=-1", "set jump-target=2", "set jump-target", "99999999999999999999999999"} {
		if _, err := a.command(input); err == nil {
			t.Fatalf("accepted %q", input)
		}
	}
	for _, input := range []string{"set ic", "set smartcase", "set incsearch", "set nohls", "set nonumber", "set nowrap", "set nomagic"} {
		if _, err := a.command(input); err != nil {
			t.Fatal(err)
		}
	}
	if !a.SmartCase || a.IgnoreCase || !a.IncrementalSearch || a.HighlightSearch || a.LineNumbers || a.Wrap || a.Magic {
		t.Fatalf("unexpected options: %+v", a)
	}
	if got, err := a.command("set noignorecase"); err != nil || got.message != "noignorecase" || a.SmartCase || a.IgnoreCase {
		t.Fatalf("set noignorecase = %+v, %v, smart %v ignore %v", got, err, a.SmartCase, a.IgnoreCase)
	}
	for _, value := range []string{".5", "0", "1"} {
		if got, err := a.command("set jump-target=" + value); err != nil || got.message != "jump-target="+value || a.JumpTarget.String() != value {
			t.Fatalf("set jump-target=%s = %+v, %v, %+v", value, got, err, a.JumpTarget)
		}
	}
}
