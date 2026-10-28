package ui

import (
	"unicode"

	"glimpse/internal/content"
)

// hexLength is the rune count of a #RRGGBB color code.
const hexLength = 7

// HexColor paints the cells of one visible #RRGGBB code with its own color.
type HexColor struct {
	Row, Column, Width int
	RGB                int32
}

// HexColors finds #RRGGBB codes delimited by non-word characters and maps them
// to rendered glyphs, like Highlights, so codes split across wrapped rows or
// partly scrolled off an unwrapped row still color their visible part.
func (t *Text) HexColors(lines []string, top int) []HexColor {
	if len(lines) == 0 {
		return nil
	}
	cells := t.sourceCells()
	if len(cells) == 0 {
		return nil
	}
	var result []HexColor
	t.scanVisible(len(lines), top, hexLength, func(start, end int64) {
		scanHexColors(t.source, start, end, func(start, end int64, rgb int32) {
			overlapping(cells, start, end, func(i int) {
				if glyph := cells[i].glyph; glyph.Width > 0 {
					result = append(result, HexColor{cells[i].row, glyph.Column, glyph.Width, rgb})
				}
			})
		})
	})
	return result
}

// scanHexColors visits each #RRGGBB code in [start,end) of decoded text. Escape
// sequences are skipped, so styling inside or around a code does not break it.
// A code touching end counts only at the end of the source; elsewhere the
// following rune is unknown.
func scanHexColors(source TextSource, start, end int64, visit func(start, end int64, rgb int32)) {
	end = min(end, source.Len())
	reader := content.NewPlainReader(source, start, end)
	previous := ' '
	matched, codeStart, codeEnd, rgb := 0, int64(0), int64(0), int32(0)
	for {
		token, err := reader.Next()
		if err != nil {
			if matched == hexLength && end == source.Len() {
				visit(codeStart, codeEnd, rgb)
			}
			return
		}
		if token.Hidden {
			continue
		}
		r := token.Rune
		if matched == hexLength {
			if !isWordRune(r) {
				visit(codeStart, codeEnd, rgb)
			}
			matched = 0
		}
		if digit, ok := hexDigit(r); ok && matched > 0 {
			matched, codeEnd = matched+1, token.End
			rgb = rgb<<4 | digit
		} else if r == '#' && !isWordRune(previous) && previous != '#' {
			matched, codeStart, rgb = 1, token.Start, 0
		} else {
			matched = 0
		}
		previous = r
	}
}

func isWordRune(r rune) bool { return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) }

func hexDigit(r rune) (int32, bool) {
	switch {
	case r >= '0' && r <= '9':
		return r - '0', true
	case r >= 'a' && r <= 'f':
		return r - 'a' + 10, true
	case r >= 'A' && r <= 'F':
		return r - 'A' + 10, true
	}
	return 0, false
}
