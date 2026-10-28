package ui

import (
	"context"
	"glimpse/internal/search"
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

// Highlights maps overlapping source matches to rendered glyphs, never to
// expanded tabs or other transformed display text. Glyph memory is viewport-bound.
func (t *Text) Highlights(lines []string, top int, query string, smart, ignore bool) []Highlight {
	if query == "" || len(lines) == 0 {
		return nil
	}
	var cells []markedGlyph
	for row, glyphs := range t.rendered {
		for _, glyph := range glyphs {
			if glyph.Start >= 0 {
				cells = append(cells, markedGlyph{glyph: glyph, row: row})
			}
		}
	}
	if len(cells) == 0 {
		return nil
	}
	sort.Slice(cells, func(i, j int) bool { return cells[i].glyph.Start < cells[j].glyph.Start })
	maximum := int64(0)
	for i := range cells {
		maximum = max(maximum, cells[i].glyph.End)
		cells[i].maxEnd = maximum
	}
	mark := func(start, end int64) bool {
		index := sort.Search(len(cells), func(i int) bool { return cells[i].maxEnd > start })
		for i := index; i < len(cells) && cells[i].glyph.Start < end; i++ {
			if cells[i].glyph.End > start {
				cells[i].marked = true
			}
		}
		return true
	}
	if t.nowrap {
		// An unwrapped row can be arbitrarily long: scan each row only up to a
		// bounded distance past its last visible glyph.
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
			end = min(end, visible+int64(len(query))*4+highlightSlack)
			_ = search.Walk(context.Background(), t.source, query, start, end, smart, ignore, mark)
		}
		return t.marked(cells)
	}
	lookaround := utf8.RuneCountInString(query)
	start, end := t.Offset(max(0, top-lookaround)), t.scanned
	if row := top + len(lines) + lookaround; row < t.RowCount() {
		end = t.Offset(row)
	}
	_ = search.Walk(context.Background(), t.source, query, start, end, smart, ignore, mark)
	return t.marked(cells)
}
