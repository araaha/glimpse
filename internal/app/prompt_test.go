package app

import (
	"testing"

	"github.com/araaha/glimpse/internal/keybind"
)

func TestPromptEditing(t *testing.T) {
	for _, tt := range []struct {
		name, text string
		cursor     int
		action     keybind.Action
		want       string
		wantCursor int
	}{
		{"left unicode", "a界b", 2, keybind.Left, "a界b", 1},
		{"right unicode", "a界b", 1, keybind.Right, "a界b", 2},
		{"start", "abc", 2, keybind.Start, "abc", 0},
		{"end", "abc", 1, keybind.End, "abc", 3},
		{"word left", "one 界界, two", 11, keybind.WordLeft, "one 界界, two", 8},
		{"word right", "one 界界, two", 3, keybind.WordRight, "one 界界, two", 6},
		{"backspace", "a界b", 2, keybind.Backspace, "ab", 1},
		{"delete", "a界b", 1, keybind.Delete, "ab", 1},
		{"delete previous word", "one 界界", 6, keybind.DeleteWordBackward, "one ", 4},
		{"delete next word", "one 界界", 3, keybind.DeleteWordForward, "one", 3},
		{"clear before", "abc def", 4, keybind.ClearBefore, "def", 0},
		{"clear after", "abc def", 3, keybind.ClearAfter, "abc", 3},
		{"backspace at start", "x", 0, keybind.Backspace, "x", 0},
		{"delete at end", "x", 1, keybind.Delete, "x", 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := newPrompt(false, nil)
			p.text, p.cursor = []rune(tt.text), tt.cursor
			p.edit(keybind.Command{Action: tt.action})
			if string(p.text) != tt.want || p.cursor != tt.wantCursor {
				t.Fatalf("prompt = %q at %d; want %q at %d", string(p.text), p.cursor, tt.want, tt.wantCursor)
			}
		})
	}
}

func TestPromptInsertionAndHistoryDraft(t *testing.T) {
	p := newPrompt(false, []string{"old", "new"})
	p.edit(keybind.Command{Action: keybind.Insert, Text: "draft界"})
	p.edit(keybind.Command{Action: keybind.HistoryPrevious})
	if string(p.text) != "new" {
		t.Fatalf("previous = %q", string(p.text))
	}
	p.edit(keybind.Command{Action: keybind.HistoryPrevious})
	if string(p.text) != "old" {
		t.Fatalf("previous = %q", string(p.text))
	}
	p.edit(keybind.Command{Action: keybind.HistoryNext})
	p.edit(keybind.Command{Action: keybind.HistoryNext})
	if string(p.text) != "draft界" || p.cursor != 6 {
		t.Fatalf("draft = %q at %d", string(p.text), p.cursor)
	}
	p.edit(keybind.Command{Action: keybind.Start})
	p.edit(keybind.Command{Action: keybind.Insert, Text: "a\x1b\nb"})
	if string(p.text) != "abdraft界" {
		t.Fatalf("unsafe insert = %q", string(p.text))
	}
}

func TestPromptMovesAndDeletesWholeGraphemes(t *testing.T) {
	p := newPrompt(false, nil)
	p.edit(keybind.Command{Action: keybind.Insert, Text: "a👩🏽‍💻é🇺🇳z"})
	for _, want := range []string{"a👩🏽‍💻é🇺🇳", "a👩🏽‍💻é", "a👩🏽‍💻", "a", ""} {
		p.edit(keybind.Command{Action: keybind.Backspace})
		if string(p.text) != want {
			t.Fatalf("grapheme backspace = %q, want %q", string(p.text), want)
		}
	}
	p.edit(keybind.Command{Action: keybind.Insert, Text: "é x"})
	p.edit(keybind.Command{Action: keybind.Start})
	p.edit(keybind.Command{Action: keybind.WordRight})
	if p.cursor != 2 {
		t.Fatalf("word motion split combining cluster at %d", p.cursor)
	}
	p.edit(keybind.Command{Action: keybind.Left})
	if p.cursor != 0 {
		t.Fatalf("left split combining cluster at %d", p.cursor)
	}
	p.edit(keybind.Command{Action: keybind.Delete})
	if string(p.text) != " x" {
		t.Fatalf("delete split combining cluster: %q", string(p.text))
	}
}
