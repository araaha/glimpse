package ui

import (
	"errors"
	"github.com/araaha/glimpse/internal/content"
	"io"
	"unicode/utf8"
)

// readStyledRow renders display columns [left, left+width) of one row. Wrapped
// rows always start at column 0; unwrapped rows are decoded from the row start
// up to the window, and Glyph columns are relative to the window.
func (t *Text) readStyledRow(start, end int64, style content.Style, underline content.Color) ([]Glyph, error) {
	left := 0
	if t.nowrap {
		left = t.left
	}
	limit := left + t.width
	if glyphs, ok, err := t.plainRow(start, end, left, limit, style, underline); ok || err != nil {
		return glyphs, err
	}
	reader := content.NewGraphemeReader(t.source, start, end, style, true, underline)
	// cells holds the window; cursor and extent are row columns.
	cells := make([]Glyph, t.width)
	cursor, extent := 0, 0
	budget := max(256, t.width*16) - t.width*utf8.UTFMax
	suffix := max(256, t.width*16)
	var struck Glyph
	visible := func(x int) bool { return x >= left && x < limit }
	put := func(text string, width int, token content.Cluster) {
		style := token.Style
		if struck.Text != "" {
			switch {
			case struck.Text == text:
				style |= content.Bold
			case struck.Text == "_":
				style |= content.Underline
			case text == "_":
				text, width, style = struck.Text, struck.Width, struck.Style|content.Underline
				token.UnderlineColor = struck.UnderlineColor
				token.Start = struck.Start
			}
		}
		struck = Glyph{}
		// Erasing either half of a wide glyph clears the whole glyph; spaces
		// preserve terminal columns instead of leaving orphan continuations.
		for x := max(cursor, left); x < min(cursor+width, limit); x++ {
			old := cells[x-left]
			if old.Width < 0 {
				old = cells[old.Column]
			}
			for j := old.Column; old.Text != "" && j < old.Column+old.Width; j++ {
				cells[j] = Glyph{Text: " ", Column: j, Width: 1, Start: -1, End: -1}
			}
		}
		switch {
		case visible(cursor) && cursor+width <= limit:
			x := cursor - left
			cells[x] = Glyph{Text: text, Column: x, Width: width, Style: style, UnderlineColor: token.UnderlineColor, Start: token.Start, End: token.End, Unprintable: token.Unprintable}
			for c := x + 1; c < x+width; c++ {
				cells[c] = Glyph{Column: x, Width: -1}
			}
		default:
			// A wide glyph cut by the window's left edge shows as blank cells.
			for x := max(cursor, left); x < min(cursor+width, limit); x++ {
				cells[x-left] = Glyph{Text: " ", Column: x - left, Width: 1, Style: style, Start: -1, End: -1}
			}
		}
		cursor += width
		extent = max(extent, cursor)
	}
	for {
		reader.SetRenderState(limit, cursor, extent, t.wideAmbiguous)
		token, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if extent >= limit && (token.Hidden || token.Rune == '\r' || token.Rune == '\b' || t.clusterWidth(token.Text) == 0) {
			suffix -= int(token.End - token.Start)
			if suffix < 0 {
				break
			}
		}
		if token.Hidden {
			continue
		}
		r := token.Rune
		if r == '\n' {
			break
		}
		if r == '\r' {
			cursor = 0
			struck = Glyph{}
			continue
		}
		if r == '\b' {
			if cursor > 0 {
				cursor--
				struck = Glyph{}
				if visible(cursor) {
					if cells[cursor-left].Width < 0 {
						cursor = left + cells[cursor-left].Column
					}
					struck = cells[cursor-left]
				}
			}
			continue
		}
		if r == '\t' {
			count := min(8-cursor%8, limit-cursor)
			if count <= 0 {
				t.overflow = t.overflow || t.nowrap
				break
			}
			for range count {
				put(" ", 1, token)
			}
			continue
		}
		width := t.clusterWidth(token.Text)
		if width == 0 {
			if cursor > left && cursor <= limit && len(token.Text) <= budget {
				x := cursor - 1 - left
				if cells[x].Width < 0 {
					x = cells[x].Column
				}
				cells[x].Text += token.Text
				cells[x].End = token.End
				budget -= len(token.Text)
			}
			continue
		}
		text := token.Text
		if token.Unprintable && t.unprintable != "" {
			text = t.unprintable
		}
		// Very long combining clusters remain a single grapheme for layout,
		// while retained terminal text stays bounded by the visible-cell budget.
		limitBytes := max(utf8.UTFMax, budget+width*utf8.UTFMax)
		if len(text) > limitBytes {
			end := limitBytes
			for end > 0 && !utf8.RuneStart(text[end]) {
				end--
			}
			text = text[:end]
		}
		budget -= min(budget, max(0, len(text)-width*utf8.UTFMax))
		if width > t.width {
			text, width = "�", 1
		}
		if cursor+width > limit {
			t.overflow = t.overflow || t.nowrap
			break
		}
		put(text, width, token)
	}
	var glyphs []Glyph
	for x := 0; x < extent-left && x < t.width; x++ {
		if cells[x].Width > 0 {
			glyphs = append(glyphs, cells[x])
		}
	}
	return glyphs, nil
}

// plainRow lays out a row of printable ASCII, one byte per column. It reports
// false when the row needs the general path.
func (t *Text) plainRow(start, end int64, left, limit int, style content.Style, underline content.Color) ([]Glyph, bool, error) {
	// Wrapped rows take the fast path only when whole; an unwrapped row reads
	// just the prefix that can reach the window.
	want := end - start
	if t.nowrap {
		want = min(want, int64(limit)+1)
	}
	if want <= 0 || !t.nowrap && want > int64(len(t.renderScratch)) {
		return nil, false, nil
	}
	if int64(cap(t.renderScratch)) < want {
		t.renderScratch = make([]byte, want)
	}
	data := t.renderScratch[:want]
	if _, err := t.source.ReadAt(data, start); err != nil {
		return nil, false, err
	}
	count := len(data)
	if data[count-1] == '\n' {
		count--
	}
	for _, b := range data[:count] {
		if b < ' ' || b > '~' {
			return nil, false, nil
		}
	}
	// A wider unwrapped row overflows; later CR/BS edits past the window are
	// ignored as in the general path.
	if count > limit {
		if !t.nowrap {
			return nil, false, nil
		}
		count, t.overflow = limit, true
	}
	var glyphs []Glyph
	for i := left; i < count; i++ {
		glyphs = append(glyphs, Glyph{Text: string(data[i : i+1]), Column: i - left, Width: 1, Style: style, UnderlineColor: underline, Start: start + int64(i), End: start + int64(i) + 1})
	}
	return glyphs, true, nil
}
