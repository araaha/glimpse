package ui

import (
	"errors"
	"glimpse/internal/content"
	"io"
	"unicode/utf8"
)

func (t *Text) readStyledRow(start, end int64, style content.Style) ([]Glyph, error) {
	reader := content.NewReader(t.source, start, end, style, true)
	cells := make([]Glyph, t.width)
	cursor, extent := 0, 0
	budget := max(256, t.width*16) - t.width*utf8.UTFMax
	suffix := max(256, t.width*16)
	var struck Glyph
	put := func(text string, width int, token content.Token) {
		style := token.Style
		if struck.Text != "" {
			switch {
			case struck.Text == text:
				style |= content.Bold
			case struck.Text == "_":
				style |= content.Underline
			case text == "_":
				text, width, style = struck.Text, struck.Width, struck.Style|content.Underline
				token.Start = struck.Start
			}
		}
		struck = Glyph{}
		// Erasing either half of a wide glyph clears the whole glyph; spaces
		// preserve terminal columns instead of leaving orphan continuations.
		for x := cursor; x < cursor+width; x++ {
			old := cells[x]
			if old.Width < 0 {
				old = cells[old.Column]
			}
			for j := old.Column; old.Text != "" && j < old.Column+old.Width; j++ {
				cells[j] = Glyph{Text: " ", Column: j, Width: 1, Start: -1, End: -1}
			}
		}
		cells[cursor] = Glyph{Text: text, Column: cursor, Width: width, Style: style, Start: token.Start, End: token.End}
		for x := cursor + 1; x < cursor+width; x++ {
			cells[x] = Glyph{Column: cursor, Width: -1}
		}
		cursor += width
		extent = max(extent, cursor)
	}
	for {
		token, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if extent >= t.width && (token.Hidden || token.Rune == '\r' || token.Rune == '\b' || runeWidth(token.Rune) == 0) {
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
				if cells[cursor].Width < 0 {
					cursor = cells[cursor].Column
				}
				struck = cells[cursor]
			}
			continue
		}
		if r == '\t' {
			count := min(8-cursor%8, t.width-cursor)
			for range count {
				put(" ", 1, token)
			}
			continue
		}
		width := runeWidth(r)
		if width == 0 {
			if cursor > 0 && utf8.RuneLen(r) <= budget {
				x := cursor - 1
				if cells[x].Width < 0 {
					x = cells[x].Column
				}
				cells[x].Text += string(r)
				cells[x].End = token.End
				budget -= utf8.RuneLen(r)
			}
			continue
		}
		text := string(r)
		if width > t.width {
			text, width = "�", 1
		}
		if cursor+width > t.width {
			break
		}
		put(text, width, token)
	}
	var glyphs []Glyph
	for x := 0; x < extent; x++ {
		if cells[x].Width > 0 {
			glyphs = append(glyphs, cells[x])
		}
	}
	return glyphs, nil
}
