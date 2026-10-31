package app

import (
	"fmt"
	"slices"
	"strings"

	"github.com/araaha/glimpse/internal/content"
	"github.com/araaha/glimpse/internal/ui"
)

// mark is a position in the document: the offset at the jump target, which
// survives wrapping, filtering, and appended input, and its line, which is
// used instead once a reload has replaced the content. A mark set while a
// line jump is pending has only the line (offset -1); one set while the index
// has not reached its offset has no line (0).
type mark struct {
	offset     int64
	line       int
	generation int64
}

// markState holds the marks a to z and the position before the last jump.
type markState struct {
	named       map[string]mark
	previous    mark
	hasPrevious bool
}

func validMark(name string) bool { return len(name) == 1 && name[0] >= 'a' && name[0] <= 'z' }

// here is a mark at the jump target, or at the target of a pending jump.
func (s *session) here() mark {
	m := mark{offset: -1, generation: s.loadedGeneration}
	switch {
	case s.filter.pendingLine > 0:
		m.line = s.filter.pendingLine
	case s.pendingLine > 0:
		m.line = s.realLine(s.pendingLine)
	case s.filter.hasPendingOffset:
		m.offset, m.line = s.filter.pendingOffset, s.lineOf(s.filter.pendingOffset)
	case s.hasPendingOffset:
		offset := s.realOffset(s.pendingOffset)
		m.offset, m.line = offset, s.lineOf(offset)
	default:
		m.offset, m.line = s.targetOffset(), s.targetLine()
	}
	return m
}

// documentText is the index of the whole document, even while filtered.
func (s *session) documentText() *ui.Text {
	if s.filtered() {
		return s.filter.plain
	}
	return s.text
}

// lineOf is the line holding a document offset, or 0 if not yet indexed.
func (s *session) lineOf(offset int64) int {
	text := s.documentText()
	if offset >= text.Indexed() {
		return 0
	}
	return text.LogicalLineAt(offset)
}

// resolve fills in the offset of a mark that has only its line, once the
// index has reached that line.
func (s *session) resolve(m mark) mark {
	text := s.documentText()
	if m.offset < 0 && m.generation == s.loadedGeneration && m.line > 0 && m.line <= text.LineCount() {
		m.offset = text.Offset(text.LineRow(m.line))
	}
	return m
}

// remember records the position a jump is about to leave, for ”.
func (s *session) remember() { s.marks.previous, s.marks.hasPrevious = s.here(), true }

func (s *session) setMark(name string) {
	if !validMark(name) {
		s.search.message = "Marks are named a to z"
		return
	}
	if s.marks.named == nil {
		s.marks.named = make(map[string]mark)
	}
	s.marks.named[name] = s.here()
}

// jumpToMark goes to a mark, or with ' to the position before the last jump.
func (s *session) jumpToMark(name string) {
	var target mark
	var ok bool
	switch {
	case name == "'":
		target, ok = s.marks.previous, s.marks.hasPrevious
		if !ok {
			s.search.message = "No previous position"
			return
		}
	case !validMark(name):
		s.search.message = "Marks are named a to z"
		return
	default:
		if target, ok = s.marks.named[name]; !ok {
			s.search.message = fmt.Sprintf("Mark %s is not set", name)
			return
		}
	}
	target = s.resolve(target)
	byLine := target.generation != s.loadedGeneration || target.offset < 0
	switch {
	case byLine && target.line == 0:
		s.search.message = fmt.Sprintf("Mark %s was lost when the input was reloaded", name)
		return
	case !byLine && target.offset < s.Document.Start():
		s.search.message = fmt.Sprintf("Mark %s was discarded by rolling retention", name)
		return
	}
	s.remember()
	s.clearPending()
	s.view.follow, s.view.bottom = false, false
	switch {
	case byLine:
		s.jumpLine(target.line)
	case s.filtered():
		s.filter.pendingOffset, s.filter.hasPendingOffset = target.offset, true
		if shown, known := s.filter.source.Shows(target.offset); known && !shown {
			s.search.message = fmt.Sprintf("Line %d is filtered out; showing the next matching line", target.line)
		}
	default:
		s.pendingOffset, s.hasPendingOffset, s.pendingPlace = target.offset, true, true
	}
}

// deleteMarks removes the named marks, or all of them for "!".
func (s *session) deleteMarks(names string) {
	if names == "!" {
		s.marks.named = nil
		return
	}
	for _, name := range names {
		delete(s.marks.named, string(name))
	}
}

// markLines lists the marks for the :marks screen.
func (s *session) markLines() []string {
	if len(s.marks.named) == 0 && !s.marks.hasPrevious {
		return []string{"No marks are set", "", "m and a letter marks the line at the jump target."}
	}
	lines := []string{"Marks (q or Esc closes this list)", "", "mark   line  text"}
	names := make([]string, 0, len(s.marks.named))
	for name := range s.marks.named {
		names = append(names, name)
	}
	slices.Sort(names)
	add := func(name string, m mark) {
		line := "?"
		if m.line > 0 {
			line = fmt.Sprint(m.line)
		}
		lines = append(lines, fmt.Sprintf(" %-3s %7s  %s", name, line, s.markText(m)))
	}
	for _, name := range names {
		add(name, s.marks.named[name])
	}
	if s.marks.hasPrevious {
		add("'", s.marks.previous)
	}
	return lines
}

// markText is the visible text of a mark's row, as far as one screen width.
func (s *session) markText(m mark) string {
	m = s.resolve(m)
	if m.generation != s.loadedGeneration {
		return "(reloaded)"
	}
	if m.offset < 0 {
		return ""
	}
	if m.offset < s.Document.Start() {
		return "(discarded)"
	}
	reader := content.NewPlainReader(s.Document, m.offset, min(s.Document.Len(), m.offset+4096))
	var text strings.Builder
	for width := 0; width < s.layout.Body.Width; width++ {
		token, err := reader.Next()
		if err != nil || token.Raw == '\n' {
			break
		}
		switch {
		case token.Hidden:
			width--
		case token.Unprintable || token.Rune < ' ':
			text.WriteRune(' ')
		default:
			text.WriteRune(token.Rune)
		}
	}
	return text.String()
}
