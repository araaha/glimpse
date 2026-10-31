package ui

import (
	"context"
	"github.com/araaha/glimpse/internal/search"
	"sort"
	"unicode/utf8"
)

// highlightSlack covers hidden escapes between the last visible glyph and the
// end of a match that starts on screen.
const highlightSlack = 64 << 10

type markedGlyph struct {
	glyph  Glyph
	row    int
	marked bool
	maxEnd int64
}

func (t *Text) marked(cells []markedGlyph) []Highlight {
	var result []Highlight
	for _, cell := range cells {
		if cell.marked && cell.glyph.Width > 0 {
			result = append(result, Highlight{cell.row, cell.glyph.Column, cell.glyph.Width})
		}
	}
	return result
}

// sourceCells lists the rendered glyphs that came from source text, ordered by
// source offset, with the running maximum end used by overlapping.
func (t *Text) sourceCells() []markedGlyph {
	var cells []markedGlyph
	for row, glyphs := range t.rendered {
		for _, glyph := range glyphs {
			if glyph.Start >= 0 {
				cells = append(cells, markedGlyph{glyph: glyph, row: row})
			}
		}
	}
	sort.Slice(cells, func(i, j int) bool { return cells[i].glyph.Start < cells[j].glyph.Start })
	maximum := int64(0)
	for i := range cells {
		maximum = max(maximum, cells[i].glyph.End)
		cells[i].maxEnd = maximum
	}
	return cells
}

// overlapping calls visit with the index of each cell overlapping [start,end).
func overlapping(cells []markedGlyph, start, end int64, visit func(int)) {
	index := sort.Search(len(cells), func(i int) bool { return cells[i].maxEnd > start })
	for i := index; i < len(cells) && cells[i].glyph.Start < end; i++ {
		if cells[i].glyph.End > start {
			visit(i)
		}
	}
}

// scanVisible calls scan over the source ranges that can hold a span of up to
// runes runes touching the screen. Wrapped text is scanned as one range reaching
// runes rows beyond the screen. An unwrapped row can be arbitrarily long, so
// each is scanned only up to a bounded distance past its last visible glyph.
func (t *Text) scanVisible(rows, top, runes int, scan func(start, end int64)) {
	if t.nowrap {
		for row, glyphs := range t.rendered {
			if len(glyphs) == 0 {
				continue
			}
			start, end := t.Offset(top+row), t.scanned
			if next := top + row + 1; next < t.RowCount() {
				end = t.Offset(next)
			}
			visible := int64(0)
			for _, glyph := range glyphs {
				visible = max(visible, glyph.End)
			}
			scan(start, min(end, visible+int64(runes)*4+highlightSlack))
		}
		return
	}
	start, end := t.Offset(max(0, top-runes)), t.scanned
	if row := top + rows + runes; row < t.RowCount() {
		end = t.Offset(row)
	}
	scan(start, end)
}

// Highlights maps overlapping source matches to rendered glyphs, never to
// expanded tabs or other transformed display text. Glyph memory is viewport-bound.
func (t *Text) Highlights(lines []string, top int, query search.Query) []Highlight {
	if query.Text == "" || len(lines) == 0 {
		return nil
	}
	cells := t.sourceCells()
	if len(cells) == 0 {
		return nil
	}
	mark := func(start, end int64) bool {
		overlapping(cells, start, end, func(i int) { cells[i].marked = true })
		return true
	}
	walk := func(start, end int64) { _ = search.Walk(context.Background(), t.source, query, start, end, mark) }
	switch {
	case !query.IsRegex():
		t.scanVisible(len(lines), top, utf8.RuneCountInString(query.Text), walk)
	case t.nowrap:
		t.scanVisible(len(lines), top, search.MaxMatchBytes/4, walk)
	default:
		t.scanLines(len(lines), top, walk)
	}
	return t.marked(cells)
}

// scanLines calls scan over the whole logical lines touching the screen, as
// regular expression matches need a line's start and end. On a very long line
// it starts at a row boundary and stops a bounded distance from the screen.
func (t *Text) scanLines(rows, top int, scan func(start, end int64)) {
	first, last := t.Offset(top), t.scanned
	if row := top + rows; row < t.RowCount() {
		last = t.Offset(row)
	}
	reach := int64(4 * search.MaxMatchBytes)
	start := t.Offset(t.RowAt(max(t.base, first-reach)))
	end := min(t.scanned, last+reach)
	if t.lineStarts != nil {
		line := t.LogicalLineAt(first)
		start = max(start, t.lineStarts.at(max(0, line-1)))
		if next := t.LogicalLineAt(max(first, last-1)); next < t.lineStarts.count {
			end = min(end, t.lineStarts.at(next))
		}
	}
	scan(start, end)
}
