package ui

import (
	"errors"
	"io"
	"sort"
	"strings"

	"glimpse/internal/content"
	"glimpse/internal/pager"

	"github.com/clipperhouse/displaywidth"
)

type TextSource interface {
	io.ReaderAt
	Len() int64
}

// Text incrementally indexes wrapped rows by byte offset, not by copying whole
// logical lines. One shared grapheme decoder drives indexing and rendering.
// The last live cluster is provisional and reflowed when later bytes extend it.
type Text struct {
	source                         TextSource
	starts                         *offsetIndex
	lineStarts                     *offsetIndex
	rowStyles                      *offsetIndex
	rowUnderlines, styleUnderlines *offsetIndex
	styleOffsets, styleValues      *offsetIndex
	style                          content.Style
	underlineColor                 content.Color
	cellStarts                     []int
	rendered                       [][]Glyph
	indexMemory, indexQuota        int64
	base                           int64
	scanned                        int64
	width                          int
	column                         int
	struckWidth                    int
	open                           *scanCheckpoint
	observedEnd                    int64
	wideAmbiguous, policyChanged   bool
}

type scanCheckpoint struct {
	scanned        int64
	style          content.Style
	underlineColor content.Color
	column, struck int
	cells          []int
	counts         [7]int
}

func (t *Text) indexes() [7]*offsetIndex {
	return [7]*offsetIndex{t.starts, t.lineStarts, t.rowStyles, t.styleOffsets, t.styleValues, t.rowUnderlines, t.styleUnderlines}
}

func (t *Text) checkpoint() *scanCheckpoint {
	saved := &scanCheckpoint{scanned: t.scanned, style: t.style, underlineColor: t.underlineColor, column: t.column, struck: t.struckWidth, cells: append([]int(nil), t.cellStarts...)}
	for j, index := range t.indexes() {
		saved.counts[j] = index.count
	}
	return saved
}

func (t *Text) rewind() error {
	saved := t.open
	for j, index := range t.indexes() {
		if err := index.truncate(saved.counts[j]); err != nil {
			return err
		}
	}
	t.scanned, t.style, t.column, t.struckWidth = saved.scanned, saved.style, saved.column, saved.struck
	t.underlineColor = saved.underlineColor
	copy(t.cellStarts, saved.cells)
	t.open = nil
	return nil
}

func (t *Text) SetWideAmbiguous(wide bool) {
	if t.wideAmbiguous != wide {
		t.wideAmbiguous, t.policyChanged = wide, true
	}
}
func (t *Text) clusterWidth(text string) int {
	return (displaywidth.Options{EastAsianWidth: t.wideAmbiguous}).String(text)
}

func NewText(source TextSource) *Text {
	text, _ := NewTextWithLimits(source, 8<<20, 256<<20)
	return text
}

func NewTextWithLimits(source TextSource, memory, quota int64) (*Text, error) {
	if memory < 16<<10 || quota < 56 {
		return nil, errors.New("metadata-budget must be at least 16384 and metadata-quota at least 56 bytes")
	}
	return &Text{source: source, indexMemory: memory, indexQuota: quota}, nil
}

func (t *Text) Close() error {
	var result error
	for _, index := range t.indexes() {
		if index != nil {
			result = errors.Join(result, index.close())
		}
	}
	return result
}

func (t *Text) reset(width int, base int64, initial content.Style, underline content.Color) error {
	if err := t.Close(); err != nil {
		return err
	}
	t.width, t.column, t.scanned, t.base = width, 0, base, base
	t.style = initial
	t.underlineColor = underline
	t.open, t.policyChanged = nil, false
	t.struckWidth = 0
	t.cellStarts = make([]int, width)
	t.starts = newOffsetIndex(t.indexMemory/7, t.indexQuota/7)
	t.lineStarts = newOffsetIndex(t.indexMemory/7, t.indexQuota/7)
	t.rowStyles = newOffsetIndex(t.indexMemory/7, t.indexQuota/7)
	t.styleOffsets = newOffsetIndex(t.indexMemory/7, t.indexQuota/7)
	t.styleValues = newOffsetIndex(t.indexMemory/7, t.indexQuota/7)
	t.rowUnderlines = newOffsetIndex(t.indexMemory/7, t.indexQuota/7)
	t.styleUnderlines = newOffsetIndex(t.indexMemory/7, t.indexQuota/7)
	if err := t.starts.append(base); err != nil {
		return err
	}
	if err := t.rowStyles.append(int64(initial)); err != nil {
		return err
	}
	if err := t.styleOffsets.append(base); err != nil {
		return err
	}
	if err := t.styleValues.append(int64(initial)); err != nil {
		return err
	}
	if err := t.rowUnderlines.append(int64(underline)); err != nil {
		return err
	}
	if err := t.styleUnderlines.append(int64(underline)); err != nil {
		return err
	}
	return t.lineStarts.append(base)
}

func (t *Text) styleAt(offset int64) (content.Style, content.Color) {
	if t.styleOffsets == nil {
		return 0, 0
	}
	index := max(0, sort.Search(t.styleOffsets.count, func(i int) bool { return t.styleOffsets.at(i) > offset })-1)
	return content.Style(t.styleValues.at(index)), content.Color(t.styleUnderlines.at(index))
}

func (t *Text) recordStyle(offset int64, style content.Style, underline content.Color) error {
	if style == t.style && underline == t.underlineColor {
		return nil
	}
	if err := t.styleOffsets.append(offset); err != nil {
		return err
	}
	if err := t.styleValues.append(int64(style)); err != nil {
		return err
	}
	if err := t.styleUnderlines.append(int64(underline)); err != nil {
		return err
	}
	t.style, t.underlineColor = style, underline
	return nil
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
	if t.width != width || t.base != base || t.policyChanged {
		initial, underline := t.styleAt(base)
		if t.styleOffsets != nil {
			if err := errors.Join(t.styleOffsets.err, t.styleValues.err, t.styleUnderlines.err); err != nil {
				return err
			}
		}
		if err := t.reset(width, base, initial, underline); err != nil {
			return err
		}
	}
	for _, index := range t.indexes() {
		if index.err != nil {
			return index.err
		}
	}
	end := t.source.Len()
	if t.open != nil {
		if end == t.observedEnd && !complete {
			return nil
		}
		if err := t.rewind(); err != nil {
			return err
		}
	}
	reader := content.NewGraphemeReader(t.source, t.scanned, end, t.style, complete, t.underlineColor)
	for {
		token, err := reader.Next()
		if errors.Is(err, io.EOF) || errors.Is(err, content.ErrIncomplete) || errors.Is(err, pager.ErrEvicted) {
			break
		}
		if err != nil {
			return err
		}
		if token.Open {
			t.open = t.checkpoint()
		}
		if err := t.recordStyle(token.Start, token.Style, token.UnderlineColor); err != nil {
			return err
		}
		if !token.Hidden {
			if err := t.addCluster(token); err != nil {
				return err
			}
		}
		if err := t.recordStyle(token.End, token.AfterStyle, token.AfterUnderlineColor); err != nil {
			return err
		}
		t.scanned = token.End
	}
	t.observedEnd = end
	return nil
}

func (t *Text) addCluster(cluster content.Cluster) error {
	r, offset, size := cluster.Rune, cluster.Start, int(cluster.End-cluster.Start)
	if r == '\n' {
		if err := t.starts.append(offset + int64(size)); err != nil {
			return err
		}
		if err := t.lineStarts.append(offset + int64(size)); err != nil {
			return err
		}
		if err := t.rowStyles.append(int64(t.style)); err != nil {
			return err
		}
		if err := t.rowUnderlines.append(int64(t.underlineColor)); err != nil {
			return err
		}
		t.column = 0
		t.struckWidth = 0
		return nil
	}
	if r == '\r' {
		t.column = 0
		t.struckWidth = 0
		return nil // CR rewinds the current display row; it cannot cross a wrap.
	}
	if r == '\b' {
		if t.column > 0 {
			previous := t.column
			t.column = t.cellStarts[t.column-1]
			t.struckWidth = previous - t.column
		}
		return nil
	}
	w := t.clusterWidth(cluster.Text)
	if r == '\t' {
		w = 1
	} // A tab at the right edge must first create a new row.
	if cluster.Text == "_" {
		w = max(w, t.struckWidth)
	}
	t.struckWidth = 0
	if w > 0 && t.column > 0 && t.column+w > t.width {
		if err := t.starts.append(offset); err != nil {
			return err
		}
		if err := t.rowStyles.append(int64(t.style)); err != nil {
			return err
		}
		if err := t.rowUnderlines.append(int64(t.underlineColor)); err != nil {
			return err
		}
		t.column = 0
	}
	if r == '\t' {
		w = min(8-t.column%8, t.width-t.column)
	}
	w = min(w, t.width)
	for i := t.column; i < min(t.width, t.column+w); i++ {
		if r == '\t' {
			t.cellStarts[i] = i
		} else {
			t.cellStarts[i] = t.column
		}
	}
	t.column += w
	return nil
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
	t.rendered = nil
	for row := max(0, top); row < t.RowCount() && len(lines) < height; row++ {
		end := t.scanned
		if row+1 < t.starts.count {
			end = t.starts.at(row + 1)
		}
		start := t.starts.at(row)
		if t.starts.err != nil {
			return nil, t.starts.err
		}
		style := content.Style(t.rowStyles.at(row))
		underline := content.Color(t.rowUnderlines.at(row))
		if err := errors.Join(t.rowStyles.err, t.rowUnderlines.err); err != nil {
			return nil, err
		}
		glyphs, err := t.readStyledRow(start, end, style, underline)
		if err != nil {
			return nil, err
		}
		var line strings.Builder
		for _, glyph := range glyphs {
			line.WriteString(glyph.Text)
		}
		lines = append(lines, line.String())
		t.rendered = append(t.rendered, glyphs)
	}
	return lines, nil
}

func (t *Text) Styled() [][]Glyph { return t.rendered }
