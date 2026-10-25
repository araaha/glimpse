package ui

import (
	"bufio"
	"errors"
	"glimpse/internal/pager"
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
	source                  TextSource
	starts                  *offsetIndex
	lineStarts              *offsetIndex
	indexMemory, indexQuota int64
	base                    int64
	scanned                 int64
	width                   int
	column                  int
	buffer                  [64 * 1024]byte
}

func NewText(source TextSource) *Text {
	text, _ := NewTextWithLimits(source, 8<<20, 256<<20)
	return text
}

func NewTextWithLimits(source TextSource, memory, quota int64) (*Text, error) {
	if memory < 16<<10 || quota < 16 {
		return nil, errors.New("metadata-budget must be at least 16384 and metadata-quota at least 16 bytes")
	}
	return &Text{source: source, indexMemory: memory, indexQuota: quota}, nil
}

func (t *Text) Close() error {
	var first, second error
	if t.starts != nil {
		first = t.starts.close()
	}
	if t.lineStarts != nil {
		second = t.lineStarts.close()
	}
	return errors.Join(first, second)
}

func (t *Text) reset(width int, base int64) error {
	if err := t.Close(); err != nil {
		return err
	}
	t.width, t.column, t.scanned, t.base = width, 0, base, base
	t.starts = newOffsetIndex(t.indexMemory/2, t.indexQuota/2)
	t.lineStarts = newOffsetIndex(t.indexMemory/2, t.indexQuota/2)
	if err := t.starts.append(base); err != nil {
		return err
	}
	return t.lineStarts.append(base)
}

// Update indexes only newly available bytes unless the terminal width changed.
// complete allows an incomplete UTF-8 suffix at EOF to be shown as replacement
// characters instead of waiting for further input.
func (t *Text) Update(width int, complete bool) error {
	width = max(1, width)
	base := int64(0)
	if source, ok := t.source.(interface{ Start() int64 }); ok {
		base = source.Start()
	}
	if t.width != width || t.base != base {
		if err := t.reset(width, base); err != nil {
			return err
		}
	}
	if t.starts.err != nil {
		return t.starts.err
	}
	if t.lineStarts.err != nil {
		return t.lineStarts.err
	}
	end := t.source.Len()
	for t.scanned < end {
		want := int(min(int64(len(t.buffer)), end-t.scanned))
		n, err := t.source.ReadAt(t.buffer[:want], t.scanned)
		if err != nil && !errors.Is(err, io.EOF) {
			if errors.Is(err, pager.ErrEvicted) {
				return nil
			}
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
			if err := t.addRune(r, t.scanned+int64(used), size); err != nil {
				return err
			}
			used += size
		}
		t.scanned += int64(used)
		if used == 0 {
			break
		}
	}
	return nil
}

func (t *Text) addRune(r rune, offset int64, size int) error {
	if r == '\n' {
		if err := t.starts.append(offset + int64(size)); err != nil {
			return err
		}
		if err := t.lineStarts.append(offset + int64(size)); err != nil {
			return err
		}
		t.column = 0
		return nil
	}
	if r == '\r' {
		return nil // Treat CRLF as one line ending.
	}
	w := runeWidth(r)
	if w > 0 && t.column > 0 && t.column+w > t.width {
		if err := t.starts.append(offset); err != nil {
			return err
		}
		t.column = 0
	}
	if r == '\t' {
		w = min(8-t.column%8, t.width-t.column)
	}
	t.column += min(w, t.width)
	return nil
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
	if t.starts == nil {
		return 0
	}
	count := t.starts.count
	if count > 0 && t.starts.at(count-1) == t.scanned {
		count-- // A final newline does not create an extra empty display row.
	}
	return count
}

func (t *Text) Offset(row int) int64 {
	if t.RowCount() == 0 {
		return 0
	}
	return t.starts.at(min(max(0, row), t.RowCount()-1))
}

// RowAt preserves the approximate byte position when the screen is resized.
func (t *Text) RowAt(offset int64) int {
	if t.starts == nil {
		return 0
	}
	return max(0, sort.Search(t.starts.count, func(i int) bool { return t.starts.at(i) > offset })-1)
}

// LineRow returns the first wrapped row of a one-based logical line. Requests
// beyond the available input clamp to the last line, ignoring a final newline.
func (t *Text) LineRow(line int) int {
	if t.lineStarts == nil {
		return 0
	}
	count := t.lineStarts.count
	if count > 0 && t.lineStarts.at(count-1) == t.scanned {
		count--
	}
	if count == 0 {
		return 0
	}
	return t.RowAt(t.lineStarts.at(min(max(1, line), count) - 1))
}

func (t *Text) Lines(top, height int) ([]string, error) {
	var lines []string
	var reader *bufio.Reader
	for row := max(0, top); row < t.RowCount() && len(lines) < height; row++ {
		end := t.scanned
		if row+1 < t.starts.count {
			end = t.starts.at(row + 1)
		}
		start := t.starts.at(row)
		if t.starts.err != nil {
			return nil, t.starts.err
		}
		section := io.NewSectionReader(t.source, start, end-start)
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
