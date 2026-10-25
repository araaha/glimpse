package ui

import (
	"bufio"
	"context"
	"io"
	"sort"
	"unicode/utf8"

	"glimpse/internal/search"
)

type sourceCell struct {
	start, end         int64
	row, column, width int
}

// Highlights maps actual source matches to the visible cells. It never searches
// transformed text (expanded tabs, ignored CR, replacement controls), and retains
// only viewport-sized metadata even when raw rows contain long invisible spans.
func (t *Text) Highlights(lines []string, top int, query string, smart, ignore bool) []Highlight {
	if query == "" || len(lines) == 0 {
		return nil
	}
	var cells []sourceCell
	for row := range lines {
		start := t.Offset(top + row)
		end := t.scanned
		if top+row+1 < len(t.starts) {
			end = t.starts[top+row+1]
		}
		reader := bufio.NewReaderSize(io.NewSectionReader(t.source, start, end-start), 4096)
		column, budget := 0, max(256, t.width*16)-t.width*utf8.UTFMax
		suffix := max(256, t.width*16)
		for {
			r, size, err := reader.ReadRune()
			if err != nil || r == '\n' {
				break
			}
			offset := start
			start += int64(size)
			if column >= t.width {
				suffix -= size
				if suffix < 0 {
					break
				}
			}
			if r == '\r' {
				continue
			}
			width := runeWidth(r)
			if r == '\t' {
				width = min(8-column%8, t.width-column)
			}
			if width > 0 && column+width > t.width {
				if column == 0 {
					width = 1
				} else {
					break
				}
			}
			if width == 0 {
				if size > budget {
					continue
				}
				budget -= size
			}
			cells = append(cells, sourceCell{offset, start, row, column, width})
			column += width
		}
	}
	if len(cells) == 0 {
		return nil
	}
	marked := make([]bool, len(cells))
	padding := int64(utf8.RuneCountInString(query) * utf8.UTFMax)
	_ = search.Walk(context.Background(), t.source, query, max(0, cells[0].start-padding), min(t.scanned, cells[len(cells)-1].end+padding), smart, ignore, func(start, end int64) bool {
		index := sort.Search(len(cells), func(i int) bool { return cells[i].end > start })
		for i := index; i < len(cells) && cells[i].start < end; i++ {
			marked[i] = true
		}
		return true
	})
	var result []Highlight
	for i, c := range cells {
		if marked[i] && c.width > 0 {
			result = append(result, Highlight{c.row, c.column, c.width})
		}
	}
	return result
}
