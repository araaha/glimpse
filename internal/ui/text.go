package ui

import (
	"bytes"
	"errors"
	"io"
	"sort"
	"strings"
	"time"

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
	indexWidth                     int // 0 when rows are unwrapped logical lines
	nowrap                         bool
	left                           int
	overflow                       bool
	column                         int
	struckWidth                    int
	open                           *scanCheckpoint
	observedEnd                    int64
	wideAmbiguous, policyChanged   bool
	exhausted, finalized           bool
	renderScratch                  []byte
	trackStyles                    bool
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

// SetWrap chooses between wrapped rows and one row per logical line. Unwrapped
// rows do not depend on the width, so resizing them needs no reindex.
func (t *Text) SetWrap(wrap bool) {
	if t.nowrap == wrap {
		t.nowrap, t.policyChanged = !wrap, true
	}
}

// SetLeft scrolls unwrapped rows horizontally to start at a display column.
func (t *Text) SetLeft(column int) { t.left = max(0, column) }

// Overflow reports whether the last rendered unwrapped rows continue past the
// right edge, which bounds horizontal scrolling without measuring whole lines.
func (t *Text) Overflow() bool { return t.overflow }
func (t *Text) clusterWidth(text string) int {
	if len(text) == 1 && text[0] >= ' ' && text[0] <= '~' {
		return 1
	}
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
	_, trackStyles := source.(interface{ Start() int64 })
	if retained, ok := source.(interface{ RetainsAll() bool }); ok && retained.RetainsAll() {
		trackStyles = false
	}
	return &Text{source: source, indexMemory: memory, indexQuota: quota, trackStyles: trackStyles}, nil
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
	t.indexWidth = width
	if t.nowrap {
		t.indexWidth = 0
	}
	t.style = initial
	t.underlineColor = underline
	t.open, t.policyChanged = nil, false
	t.exhausted, t.finalized = false, false
	t.renderScratch = make([]byte, width+1)
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
	// Per-transition history is only needed to recover colors after rolling
	// eviction. Lossless input is reread from its original base on reflow;
	// wrapped-row checkpoints already preserve all rendering styles.
	if !t.trackStyles {
		t.style, t.underlineColor = style, underline
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
	return t.UpdateFor(width, complete, 0)
}

// NeedsUpdate distinguishes an unfinished index from a live partial UTF-8 or
// control token that is waiting for new input (which must not busy-loop).
func (t *Text) NeedsUpdate(width int, complete bool) bool {
	base := int64(0)
	if source, ok := t.source.(interface{ Start() int64 }); ok {
		base = source.Start()
	}
	return t.width != max(1, width) || t.policyChanged || t.base != base || !t.exhausted || t.observedEnd != t.source.Len() || complete && !t.finalized
}

func (t *Text) Indexed() int64 { return t.scanned }

// LogicalLineAt returns the one-based logical line containing a decoded offset,
// independently of wrapping. It reads only the existing UI-owned paged index.
func (t *Text) LogicalLineAt(offset int64) int {
	if t.LineCount() == 0 {
		return 0
	}
	return min(t.LineCount(), max(1, sort.Search(t.lineStarts.count, func(i int) bool { return t.lineStarts.at(i) > offset })))
}

// SearchAnchor supplies a decoded restart boundary near origin. Rewinding some
// logical lines makes backward repeats cheap without starting inside hidden
// controls, UTF-8, or a wrapped man-page overstrike sequence.
func (t *Text) SearchAnchor(origin int64) int64 {
	if t.lineStarts == nil {
		return t.base
	}
	line := max(0, sort.Search(t.lineStarts.count, func(i int) bool { return t.lineStarts.at(i) > origin })-1)
	return t.lineStarts.at(max(0, line-32))
}

// UpdateFor cooperatively yields at token boundaries. A zero budget indexes the
// whole available snapshot, preserving Update's noninteractive/test API.
func (t *Text) UpdateFor(width int, complete bool, budget time.Duration) error {
	width = max(1, width)
	base := int64(0)
	if source, ok := t.source.(interface{ Start() int64 }); ok {
		base = source.Start()
	}
	key := width
	if t.nowrap {
		key = 0
	}
	if t.indexWidth != key || t.starts == nil || t.base != base || t.policyChanged {
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
	if t.width != width {
		t.width, t.renderScratch = width, make([]byte, width+1)
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
	var deadline time.Time
	if budget > 0 {
		deadline = time.Now().Add(budget)
	}
	steps := 0
	t.exhausted, t.finalized = false, false
	for {
		if budget > 0 && steps >= 32 {
			if !time.Now().Before(deadline) {
				break
			}
			steps = 0
		}
		steps++
		if run, ok := reader.ASCII(); ok {
			if err := t.recordStyle(run.Start, run.Style, run.UnderlineColor); err != nil {
				return err
			}
			if err := t.addASCII(run.Bytes, run.Start); err != nil {
				return err
			}
			t.scanned = run.Start + int64(len(run.Bytes))
			continue
		}
		token, err := reader.Next()
		if errors.Is(err, io.EOF) || errors.Is(err, content.ErrIncomplete) || errors.Is(err, pager.ErrEvicted) {
			t.exhausted = true
			t.finalized = complete
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

func (t *Text) LineCount() int {
	if t.lineStarts == nil {
		return 0
	}
	count := t.lineStarts.count
	if count > 0 && t.lineStarts.at(count-1) == t.scanned {
		count--
	}
	return count
}

func (t *Text) addASCII(data []byte, offset int64) error {
	if t.nowrap {
		for {
			count := bytes.IndexByte(data, '\n')
			if count < 0 {
				return nil
			}
			if err := t.addCluster(content.Cluster{Token: content.Token{Rune: '\n', Start: offset + int64(count), End: offset + int64(count) + 1}, Text: "\n"}); err != nil {
				return err
			}
			data, offset = data[count+1:], offset+int64(count)+1
		}
	}
	if t.struckWidth > 0 && len(data) > 0 {
		if err := t.addCluster(content.Cluster{Token: content.Token{Rune: rune(data[0]), Start: offset, End: offset + 1}, Text: string(data[:1])}); err != nil {
			return err
		}
		data, offset = data[1:], offset+1
	}
	for len(data) > 0 {
		count := bytes.IndexByte(data, '\n')
		if count < 0 {
			count = len(data)
		}
		for count > 0 {
			if t.column == t.width {
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
			n := min(count, t.width-t.column)
			// Only the final row of this run can be edited by a later CR/BS.
			// Intermediate full rows are immutable, so don't rewrite one cell
			// checkpoint per byte just to overwrite it on the next wrapped row.
			if count == n {
				for cell := t.column; cell < t.column+n; cell++ {
					t.cellStarts[cell] = cell
				}
			}
			t.column += n
			data, offset, count = data[n:], offset+int64(n), count-n
		}
		if len(data) > 0 && data[0] == '\n' {
			if err := t.addCluster(content.Cluster{Token: content.Token{Rune: '\n', Start: offset, End: offset + 1}, Text: "\n"}); err != nil {
				return err
			}
			data, offset = data[1:], offset+1
		}
	}
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
	if t.nowrap {
		return nil // Unwrapped rows only end at newlines; columns are rendered lazily.
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
	t.rendered, t.overflow = nil, false
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

// columnScanLimit bounds the decode needed to place a match horizontally.
const columnScanLimit = 16 << 20

// ColumnAt returns the display column of offset within its row, or -1 when the
// offset lies too far into the row to measure cheaply.
func (t *Text) ColumnAt(offset int64) int {
	if t.RowCount() == 0 {
		return 0
	}
	row := t.RowAt(offset)
	start := t.Offset(row)
	if offset-start > columnScanLimit {
		return -1
	}
	reader := content.NewGraphemeReader(t.source, start, offset, content.Style(t.rowStyles.at(row)), true, content.Color(t.rowUnderlines.at(row)))
	column := 0
	for {
		token, err := reader.Next()
		if err != nil {
			return column
		}
		switch {
		case token.Hidden:
		case token.Rune == '\r':
			column = 0
		case token.Rune == '\b':
			column = max(0, column-1)
		case token.Rune == '\t':
			column += 8 - column%8
		default:
			column += t.clusterWidth(token.Text)
		}
	}
}

// LineNumberAt returns the logical line shown on row and whether the row is
// that line's first (unwrapped or first wrapped) row.
func (t *Text) LineNumberAt(row int) (int, bool) {
	if row < 0 || row >= t.RowCount() {
		return 0, false
	}
	offset := t.Offset(row)
	line := t.LogicalLineAt(offset)
	return line, line > 0 && t.lineStarts.at(line-1) == offset
}
