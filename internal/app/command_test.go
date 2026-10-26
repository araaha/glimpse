package app

import (
	"glimpse/internal/keybind"
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
	for _, input := range []string{"!rm", "0", "-1", "set unknown", "set smart-case=bad", "99999999999999999999999999"} {
		if _, err := a.command(input); err == nil {
			t.Fatalf("accepted %q", input)
		}
	}
	for _, input := range []string{"set ignore-case", "set smart-case", "set incremental", "set no-highlight"} {
		if _, err := a.command(input); err != nil {
			t.Fatal(err)
		}
	}
	if !a.SmartCase || a.IgnoreCase || !a.IncrementalSearch || a.HighlightSearch {
		t.Fatalf("unexpected options: %+v", a)
	}
}
