package app

import (
	"github.com/clipperhouse/uax29/v2/graphemes"
	"sort"
	"unicode"
	"unicode/utf8"

	"github.com/araaha/glimpse/internal/keybind"
	"github.com/araaha/glimpse/internal/search"
)

// prompt stores rune indices but only moves/deletes at grapheme boundaries.
type prompt struct {
	text         []rune
	cursor       int
	backward     bool
	command      bool
	filter       bool
	origin       int64
	from         mark // where the view was, for '' after a search
	follow       bool // the view was following when the prompt opened
	history      []string
	historyIndex int
	draft        string
	batching     bool
	completion   *completion // set while : completion cycles
}

func newPrompt(backward bool, entries []string) *prompt {
	return &prompt{backward: backward, history: append([]string(nil), entries...), historyIndex: len(entries)}
}

func wordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r) || r == '_'
}

func (p *prompt) boundaries() []int {
	result := []int{0}
	count := 0
	iterator := graphemes.FromString(string(p.text))
	for iterator.Next() {
		count += utf8.RuneCountInString(iterator.Value())
		result = append(result, count)
	}
	return result
}

func (p *prompt) previous() int {
	points := p.boundaries()
	index := sort.SearchInts(points, p.cursor)
	return points[max(0, index-1)]
}

func (p *prompt) next() int {
	points := p.boundaries()
	index := sort.Search(len(points), func(i int) bool { return points[i] > p.cursor })
	return points[min(index, len(points)-1)]
}

func (p *prompt) align(position int, forward bool) int {
	points := p.boundaries()
	index := sort.SearchInts(points, position)
	if !forward && (index == len(points) || points[index] > position) {
		index--
	}
	return points[min(max(0, index), len(points)-1)]
}

func (p *prompt) wordLeft() int {
	i := p.cursor
	for i > 0 && !wordRune(p.text[i-1]) {
		i--
	}
	for i > 0 && wordRune(p.text[i-1]) {
		i--
	}
	return p.align(i, false)
}

func (p *prompt) wordRight() int {
	i := p.cursor
	for i < len(p.text) && !wordRune(p.text[i]) {
		i++
	}
	for i < len(p.text) && wordRune(p.text[i]) {
		i++
	}
	return p.align(i, true)
}

func (p *prompt) edit(command keybind.Command) {
	if command.Action == keybind.Complete || command.Action == keybind.CompletePrevious {
		if p.command {
			p.complete(command.Action == keybind.Complete)
		}
		return
	}
	p.completion = nil
	if command.Action != keybind.Insert {
		p.cursor = p.align(p.cursor, true)
	}
	defer func() {
		if !p.batching || command.Action != keybind.Insert {
			p.cursor = p.align(p.cursor, true)
		}
	}()
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
		p.cursor = p.previous()
	case keybind.Right:
		p.cursor = p.next()
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
			start := p.previous()
			p.text = append(p.text[:start], p.text[p.cursor:]...)
			p.cursor = start
		}
	case keybind.Delete:
		if p.cursor < len(p.text) {
			p.text = append(p.text[:p.cursor], p.text[p.next():]...)
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
