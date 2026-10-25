package ui

import (
	"regexp"
	"strings"
	"unicode"
)

// Highlights searches only bounded, already-rendered viewport text. Wrapped rows
// join without newlines, while real logical line endings keep their separators.
func (t *Text) Highlights(lines []string, top int, query string, smart, ignore bool) []Highlight {
	if query == "" {
		return nil
	}
	fold := smart || ignore
	if smart {
		for _, r := range query {
			if unicode.IsUpper(r) {
				fold = false
			}
		}
	}
	pattern := regexp.QuoteMeta(query)
	if fold {
		pattern = "(?i)" + pattern
	}
	re := regexp.MustCompile(pattern)
	type cell struct{ start, end, row, column, width int }
	var cells []cell
	var text strings.Builder
	for row, line := range lines {
		column := 0
		for _, r := range line {
			start := text.Len()
			text.WriteRune(r)
			width := runeWidth(r)
			cells = append(cells, cell{start, text.Len(), row, column, width})
			column += width
		}
		if top+row+1 < t.RowCount() {
			var last [1]byte
			if _, err := t.source.ReadAt(last[:], t.Offset(top+row+1)-1); err == nil && last[0] == '\n' {
				text.WriteByte('\n')
			}
		}
	}
	var result []Highlight
	index := 0
	for _, match := range re.FindAllStringIndex(text.String(), -1) {
		for index < len(cells) && cells[index].end <= match[0] {
			index++
		}
		for i := index; i < len(cells) && cells[i].start < match[1]; i++ {
			c := cells[i]
			if c.width > 0 {
				result = append(result, Highlight{c.row, c.column, c.width})
			}
		}
	}
	return result
}
