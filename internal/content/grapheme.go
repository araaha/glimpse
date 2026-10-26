package content

import (
	"errors"
	"fmt"
	"github.com/clipperhouse/displaywidth"
	"github.com/clipperhouse/uax29/v2/graphemes"
	"io"
	"sort"
	"unicode/utf8"
)

// MaxClusterBytes is a hard safety limit, not a wrap boundary. Oversized clusters
// fail explicitly rather than silently splitting user-perceived characters or
// allocating without limit. Ordinary newline-free lines have no such limit.
const MaxClusterBytes = 64 << 10

type Cluster struct {
	Token
	Text                string
	AfterStyle          Style
	AfterUnderlineColor Color
	Open                bool // Last live cluster may grow when more input arrives.
}

// GraphemeReader batches decoded tokens, then uses the Unicode UAX #29 segmenter
// on progressively larger buffers. This avoids rescanning long combining runs
// once per rune. Source mappings and buffers remain bounded by MaxClusterBytes.
type GraphemeReader struct {
	reader                                  *Reader
	data                                    []byte
	tokens                                  []Token
	ends                                    []int64
	encoded, consumed                       int64
	lastEnd, emittedEnd                     int64
	lastStyle                               Style
	lastUnderlineColor                      Color
	eof, complete                           bool
	renderWidth, renderColumn, renderExtent int
	renderWide, checkedFull, full           bool
	hiddenBytes                             int64
}

// SetRenderState bounds invisible suffix prefetch only once a row is full.
// Leading ignored input and ordinary indexing remain unrestricted by this cap.
func (r *GraphemeReader) SetRenderState(width, column, extent int, wide bool) {
	r.renderWidth, r.renderColumn, r.renderExtent, r.renderWide = width, column, extent, wide
	r.checkedFull = false
}

func NewGraphemeReader(source io.ReaderAt, start, end int64, style Style, complete bool, underline ...Color) *GraphemeReader {
	r := &GraphemeReader{reader: NewReader(source, start, end, style, complete, underline...), complete: complete, lastEnd: start, emittedEnd: start, lastStyle: style}
	if len(underline) > 0 {
		r.lastUnderlineColor = underline[0]
	}
	return r
}

func (r *GraphemeReader) fill(target int) error {
	for len(r.data) < target && !r.eof {
		token, err := r.reader.Next()
		if errors.Is(err, io.EOF) || errors.Is(err, ErrIncomplete) {
			r.eof = true
			break
		}
		if err != nil {
			return err
		}
		r.lastEnd, r.lastStyle = token.End, token.Style
		r.lastUnderlineColor = token.UnderlineColor
		if token.Hidden {
			if r.renderWidth > 0 {
				if !r.checkedFull {
					width := (displaywidth.Options{EastAsianWidth: r.renderWide}).String(string(r.data))
					r.full = r.renderExtent >= r.renderWidth || r.renderColumn+width >= r.renderWidth
					r.checkedFull = true
				}
				if r.full {
					r.hiddenBytes += token.End - token.Start
					if r.hiddenBytes > int64(max(256, r.renderWidth*16)) {
						r.eof = true
						break
					}
				}
			}
			continue
		}
		r.hiddenBytes, r.checkedFull = 0, false
		r.data = utf8.AppendRune(r.data, token.Rune)
		r.encoded += int64(utf8.RuneLen(token.Rune))
		r.tokens = append(r.tokens, token)
		r.ends = append(r.ends, r.encoded)
	}
	return nil
}

func (r *GraphemeReader) Next() (Cluster, error) {
	if err := r.fill(256); err != nil {
		return Cluster{}, err
	}
	if len(r.data) == 0 {
		if r.lastEnd > r.emittedEnd {
			token := Token{Start: r.emittedEnd, End: r.lastEnd, Style: r.lastStyle, Hidden: true, UnderlineColor: r.lastUnderlineColor}
			r.emittedEnd = r.lastEnd
			return Cluster{Token: token, AfterStyle: r.lastStyle, AfterUnderlineColor: r.lastUnderlineColor}, nil
		}
		return Cluster{}, io.EOF
	}
	advance := 0
	control := r.tokens[0].Rune == '\r' || r.tokens[0].Rune == '\n' || r.tokens[0].Rune == '\b' || r.tokens[0].Rune == '\t'
	if control {
		advance = utf8.RuneLen(r.tokens[0].Rune)
	}
	for advance == 0 {
		var err error
		advance, _, err = graphemes.SplitFunc(r.data, r.eof)
		if err != nil {
			return Cluster{}, err
		}
		// Keep CR/LF/BS/TAB separate for explicit pager row semantics.
		if advance > 0 {
			break
		}
		if len(r.data) >= MaxClusterBytes+utf8.UTFMax {
			return Cluster{}, fmt.Errorf("grapheme cluster exceeds %d-byte safety limit", MaxClusterBytes)
		}
		if err := r.fill(min(MaxClusterBytes+utf8.UTFMax, max(512, len(r.data)*2))); err != nil {
			return Cluster{}, err
		}
	}
	if advance > MaxClusterBytes {
		return Cluster{}, fmt.Errorf("grapheme cluster exceeds %d-byte safety limit", MaxClusterBytes)
	}
	end := r.consumed + int64(advance)
	count := sort.Search(len(r.ends), func(i int) bool { return r.ends[i] >= end }) + 1
	token := r.tokens[0]
	token.End = r.tokens[count-1].End
	after := r.tokens[count-1].Style
	afterUnderline := r.tokens[count-1].UnderlineColor
	last := count == len(r.tokens) && r.eof
	if last && !control {
		token.End, after = r.lastEnd, r.lastStyle
		afterUnderline = r.lastUnderlineColor
	}
	cluster := Cluster{Token: token, Text: string(r.data[:advance]), AfterStyle: after, AfterUnderlineColor: afterUnderline, Open: last && !r.complete && !control}
	r.data, r.tokens, r.ends = r.data[advance:], r.tokens[count:], r.ends[count:]
	r.consumed, r.emittedEnd = end, token.End
	return cluster, nil
}
