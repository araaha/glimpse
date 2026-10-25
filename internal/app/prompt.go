package app

import (
	"unicode"
	"unicode/utf8"

	"glimpse/internal/keybind"
	"glimpse/internal/search"
)

// prompt uses rune indices, never byte indices, for editing and word movement.
type prompt struct {
	text         []rune
	cursor       int
	backward     bool
	command      bool
	origin       int64
	history      []string
	historyIndex int
	draft        string
}

func newPrompt(backward bool, entries []string) *prompt {
	return &prompt{backward: backward, history: append([]string(nil), entries...), historyIndex: len(entries)}
}

func wordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' }

func (p *prompt) wordLeft() int {
	i := p.cursor
	for i > 0 && !wordRune(p.text[i-1]) {
		i--
	}
	for i > 0 && wordRune(p.text[i-1]) {
		i--
	}
	return i
}

func (p *prompt) wordRight() int {
	i := p.cursor
	for i < len(p.text) && !wordRune(p.text[i]) {
		i++
	}
	for i < len(p.text) && wordRune(p.text[i]) {
		i++
	}
	return i
}

func (p *prompt) edit(command keybind.Command) {
	switch command.Action {
	case keybind.Insert:
		if utf8.ValidString(command.Text) && len(string(p.text))+len(command.Text) <= search.MaxQueryBytes {
			for _, r := range command.Text {
				if unicode.IsControl(r) {
					continue
				}
				p.text = append(p.text, 0)
				copy(p.text[p.cursor+1:], p.text[p.cursor:])
				p.text[p.cursor] = r
				p.cursor++
			}
		}
	case keybind.Left:
		p.cursor = max(0, p.cursor-1)
	case keybind.Right:
		p.cursor = min(len(p.text), p.cursor+1)
	case keybind.Start:
		p.cursor = 0
	case keybind.End:
		p.cursor = len(p.text)
	case keybind.WordLeft:
		p.cursor = p.wordLeft()
	case keybind.WordRight:
		p.cursor = p.wordRight()
	case keybind.Backspace:
		if p.cursor > 0 {
			p.text = append(p.text[:p.cursor-1], p.text[p.cursor:]...)
			p.cursor--
		}
	case keybind.Delete:
		if p.cursor < len(p.text) {
			p.text = append(p.text[:p.cursor], p.text[p.cursor+1:]...)
		}
	case keybind.DeleteWordBackward:
		start := p.wordLeft()
		p.text = append(p.text[:start], p.text[p.cursor:]...)
		p.cursor = start
	case keybind.DeleteWordForward:
		p.text = append(p.text[:p.cursor], p.text[p.wordRight():]...)
	case keybind.ClearBefore:
		p.text = append([]rune(nil), p.text[p.cursor:]...)
		p.cursor = 0
	case keybind.ClearAfter:
		p.text = p.text[:p.cursor]
	case keybind.HistoryPrevious, keybind.HistoryNext:
		if p.historyIndex == len(p.history) {
			p.draft = string(p.text)
		}
		if command.Action == keybind.HistoryPrevious {
			p.historyIndex = max(0, p.historyIndex-1)
		} else {
			p.historyIndex = min(len(p.history), p.historyIndex+1)
		}
		text := p.draft
		if p.historyIndex < len(p.history) {
			text = p.history[p.historyIndex]
		}
		p.text = []rune(text)
		p.cursor = len(p.text)
	}
}
