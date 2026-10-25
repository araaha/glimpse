package ui

import (
	"bufio"
	"errors"
	"io"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/clipperhouse/displaywidth"
)

type TextSource interface {
	io.ReaderAt
	Len() int64
}

// Text incrementally indexes wrapped rows by byte offset, not by copying whole
// logical lines. An unfinished line is available immediately. Resizing rebuilds
// the row index; full grapheme-aware reflow and ANSI styling are future work.
type Text struct {
	source     TextSource
	starts     []int64
	lineStarts []int64
	scanned    int64
	width      int
	column     int
	buffer     [64 * 1024]byte
}

func NewText(source TextSource) *Text { return &Text{source: source} }

// Update indexes only newly available bytes unless the terminal width changed.
// complete allows an incomplete UTF-8 suffix at EOF to be shown as replacement
// characters instead of waiting for further input.
func (t *Text) Update(width int, complete bool) error {
	width = max(1, width)
	if t.width != width {
		t.width, t.column, t.scanned = width, 0, 0
		t.starts = []int64{0}
		t.lineStarts = []int64{0}
	}
	end := t.source.Len()
	for t.scanned < end {
		want := int(min(int64(len(t.buffer)), end-t.scanned))
		n, err := t.source.ReadAt(t.buffer[:want], t.scanned)
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		used := 0
		for used < n {
			r, size := rune(t.buffer[used]), 1
			if r >= utf8.RuneSelf {
				if !utf8.FullRune(t.buffer[used:n]) && (!complete || t.scanned+int64(n) < end) {
					break
				}
				r, size = utf8.DecodeRune(t.buffer[used:n])
			}
			t.addRune(r, t.scanned+int64(used), size)
			used += size
		}
		t.scanned += int64(used)
		if used == 0 {
			break
		}
	}
	return nil
}

func (t *Text) addRune(r rune, offset int64, size int) {
	if r == '\n' {
		t.starts = append(t.starts, offset+int64(size))
		t.lineStarts = append(t.lineStarts, offset+int64(size))
		t.column = 0
		return
	}
	if r == '\r' {
		return // Treat CRLF as one line ending.
	}
	w := runeWidth(r)
	if w > 0 && t.column > 0 && t.column+w > t.width {
		t.starts = append(t.starts, offset)
		t.column = 0
	}
	if r == '\t' {
		w = min(8-t.column%8, t.width-t.column)
	}
	t.column += min(w, t.width)
}

func runeWidth(r rune) int {
	if r == '\t' || unicode.IsControl(r) {
		return 1
	}
	if r < utf8.RuneSelf {
		return 1
	}
	return displaywidth.Rune(r)
}

func (t *Text) RowCount() int {
	count := len(t.starts)
	if count > 0 && t.starts[count-1] == t.scanned {
		count-- // A final newline does not create an extra empty display row.
	}
	return count
}

func (t *Text) Offset(row int) int64 {
	if t.RowCount() == 0 {
		return 0
	}
	return t.starts[min(max(0, row), t.RowCount()-1)]
}

// RowAt preserves the approximate byte position when the screen is resized.
func (t *Text) RowAt(offset int64) int {
	return max(0, sort.Search(len(t.starts), func(i int) bool { return t.starts[i] > offset })-1)
}

// LineRow returns the first wrapped row of a one-based logical line. Requests
// beyond the available input clamp to the last line, ignoring a final newline.
func (t *Text) LineRow(line int) int {
	count := len(t.lineStarts)
	if count > 0 && t.lineStarts[count-1] == t.scanned {
		count--
	}
	if count == 0 {
		return 0
	}
	return t.RowAt(t.lineStarts[min(max(1, line), count)-1])
}

func (t *Text) Lines(top, height int) ([]string, error) {
	var lines []string
	var reader *bufio.Reader
	for row := max(0, top); row < t.RowCount() && len(lines) < height; row++ {
		end := t.scanned
		if row+1 < len(t.starts) {
			end = t.starts[row+1]
		}
		section := io.NewSectionReader(t.source, t.starts[row], end-t.starts[row])
		if reader == nil {
			reader = bufio.NewReaderSize(section, 4096)
		} else {
			reader.Reset(section)
		}
		line, err := t.readLine(reader)
		if err != nil {
			return nil, err
		}
		lines = append(lines, line)
	}
	return lines, nil
}

func (t *Text) readLine(reader *bufio.Reader) (string, error) {
	var line strings.Builder
	column := 0
	maxBytes := max(256, t.width*16)
	// Limit retained zero-width text, not bytes scanned before visible cells.
	// Reserve enough space for every remaining cell even after a long span of
	// combining marks. Ignored carriage returns consume no output budget.
	zeroWidthBudget := maxBytes - t.width*utf8.UTFMax
	suffixBudget := maxBytes
	for {
		r, size, err := reader.ReadRune()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", err
		}
		if r == '\n' {
			break
		}
		if column >= t.width {
			// The visible cells are full; retain only a bounded combining
			// suffix rather than scanning an arbitrarily long invisible tail.
			suffixBudget -= size
			if suffixBudget < 0 {
				break
			}
		}
		if r == '\r' {
			continue
		}
		if r == '\t' {
			spaces := min(8-column%8, t.width-column)
			line.WriteString(strings.Repeat(" ", spaces))
			column += spaces
			continue
		}
		if unicode.IsControl(r) {
			r = utf8.RuneError // Show controls instead of executing escapes.
		}
		w := runeWidth(r)
		if w > 0 && column+w > t.width {
			if column == 0 { // A wide character in a one-column terminal.
				line.WriteRune(utf8.RuneError)
			}
			break
		}
		if w == 0 {
			bytes := utf8.RuneLen(r)
			if bytes > zeroWidthBudget {
				if column >= t.width {
					break
				}
				continue
			}
			zeroWidthBudget -= bytes
		}
		line.WriteRune(r)
		column += w
	}
	return line.String(), nil
}
