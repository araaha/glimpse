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
	dataStart, tokenStart                   int
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

// ASCII batches only proven one-cell graphemes and newlines. A final printable
// byte stays on Next unless buffered lookahead proves its grapheme boundary.
// Bytes is valid until the next reader operation.
func (r *GraphemeReader) ASCII() (ASCIIRun, bool) {
	if r.dataStart < len(r.data) {
		data, tokens := r.data[r.dataStart:], r.tokens[r.tokenStart:]
		n := asciiPrefix(data)
		if n == 0 && len(data) == 1 && data[0] >= ' ' && data[0] <= '~' {
			peek, _ := r.reader.reader.Peek(1)
			if len(peek) > 0 && (peek[0] >= ' ' && peek[0] <= '~' || peek[0] == '\n' || peek[0] == '\r' || peek[0] == '\b' || peek[0] == '\t') {
				n = 1
			}
		}
		if n == 0 {
			return ASCIIRun{}, false
		}
		first := tokens[0]
		for i := 0; i < n; i++ {
			token := tokens[i]
			if token.Start != first.Start+int64(i) || token.End != token.Start+1 || token.Style != first.Style || token.UnderlineColor != first.UnderlineColor {
				n = i
				break
			}
		}
		if n == 0 {
			return ASCIIRun{}, false
		}
		r.dataStart += n
		r.tokenStart += n
		r.consumed += int64(n)
		r.emittedEnd = first.Start + int64(n)
		return ASCIIRun{Bytes: data[:n], Start: first.Start, Style: first.Style, UnderlineColor: first.UnderlineColor}, true
	}
	if r.eof {
		return ASCIIRun{}, false
	}
	// At a proven grapheme boundary, ordinary SGR can be decoded without
	// prefetching a batch of visible runes. Keep Unicode/other controls on Next.
	for {
		if r.reader.reader.Buffered() == 0 {
			r.reader.reader.Peek(1)
		}
		data, _ := r.reader.reader.Peek(r.reader.reader.Buffered())
		n := sgrPrefix(data)
		if n == 0 {
			break
		}
		token, err := r.reader.Next()
		if err != nil {
			break
		}
		r.lastEnd, r.lastStyle, r.lastUnderlineColor = token.End, token.Style, token.UnderlineColor
	}
	run, ok := r.reader.ascii()
	if ok {
		r.encoded += int64(len(run.Bytes))
		r.consumed += int64(len(run.Bytes))
		r.lastEnd, r.emittedEnd = run.Start+int64(len(run.Bytes)), run.Start+int64(len(run.Bytes))
		r.lastStyle, r.lastUnderlineColor = run.Style, run.UnderlineColor
	}
	return run, ok
}

// WideRune reports runes that are always a grapheme cluster of their own, two
// cells wide, when another such rune or any ASCII byte follows: CJK
// ideographs, kana, Hangul syllables, and fullwidth forms. None is a Prepend,
// Extend, ZWJ, or SpacingMark, and a Hangul syllable joins only Hangul jamo.
func WideRune(r rune) bool {
	return r >= 0x4e00 && r <= 0x9fff || r >= 0x3400 && r <= 0x4dbf || r >= 0x3041 && r <= 0x3096 ||
		r >= 0x30a1 && r <= 0x30fa || r >= 0xac00 && r <= 0xd7a3 || r >= 0xff01 && r <= 0xff60 || r >= 0xffe0 && r <= 0xffe6
}

// Wide batches buffered WideRune clusters of one style, like ASCII. Each
// token is one cluster; the final buffered rune stays on Next unless the rune
// after it proves its boundary. Tokens are valid until the next reader operation.
func (r *GraphemeReader) Wide() ([]Token, bool) {
	if len(r.tokens)-r.tokenStart < 2 && !r.eof {
		if err := r.fill(256); err != nil {
			return nil, false
		}
	}
	tokens := r.tokens[r.tokenStart:]
	n, size := 0, 0
	for n+1 < len(tokens) && WideRune(tokens[n].Rune) {
		token, next := tokens[n], tokens[n+1]
		if next.Rune >= utf8.RuneSelf && !WideRune(next.Rune) || n > 0 && (token.Start != tokens[n-1].End || token.Style != tokens[0].Style || token.UnderlineColor != tokens[0].UnderlineColor) {
			break
		}
		size += utf8.RuneLen(token.Rune)
		n++
	}
	if n == 0 {
		return nil, false
	}
	r.dataStart += size
	r.tokenStart += n
	r.consumed += int64(size)
	r.emittedEnd = tokens[n-1].End
	return tokens[:n], true
}

func NewGraphemeReader(source io.ReaderAt, start, end int64, style Style, complete bool, underline ...Color) *GraphemeReader {
	r := &GraphemeReader{reader: NewReader(source, start, end, style, complete, underline...), complete: complete, lastEnd: start, emittedEnd: start, lastStyle: style}
	if len(underline) > 0 {
		r.lastUnderlineColor = underline[0]
	}
	return r
}

func (r *GraphemeReader) fill(target int) error {
	// Compact only when more input is needed. Sliding the slices and topping
	// them up after every character discarded their capacity and reallocated
	// token buffers hundreds of times per input block.
	if r.dataStart > 0 {
		copy(r.data, r.data[r.dataStart:])
		r.data = r.data[:len(r.data)-r.dataStart]
		r.dataStart = 0
	}
	if r.tokenStart > 0 {
		copy(r.tokens, r.tokens[r.tokenStart:])
		r.tokens = r.tokens[:len(r.tokens)-r.tokenStart]
		copy(r.ends, r.ends[r.tokenStart:])
		r.ends = r.ends[:len(r.ends)-r.tokenStart]
		r.tokenStart = 0
	}
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
		// Two leading ASCII bytes prove the first grapheme boundary. Stop
		// lookahead here so styling near an ASCII run does not force the next
		// 256 characters through per-rune decoding instead of ASCII batching.
		if len(r.data) >= 2 && r.data[0] < utf8.RuneSelf && r.data[1] < utf8.RuneSelf {
			break
		}
	}
	return nil
}

func (r *GraphemeReader) Next() (Cluster, error) {
	if r.dataStart == len(r.data) {
		if err := r.fill(256); err != nil {
			return Cluster{}, err
		}
	}
	if r.dataStart == len(r.data) {
		if r.lastEnd > r.emittedEnd {
			token := Token{Start: r.emittedEnd, End: r.lastEnd, Style: r.lastStyle, Hidden: true, UnderlineColor: r.lastUnderlineColor}
			r.emittedEnd = r.lastEnd
			return Cluster{Token: token, AfterStyle: r.lastStyle, AfterUnderlineColor: r.lastUnderlineColor}, nil
		}
		return Cluster{}, io.EOF
	}
	data, tokens := r.data[r.dataStart:], r.tokens[r.tokenStart:]
	advance := 0
	control := tokens[0].Rune == '\r' || tokens[0].Rune == '\n' || tokens[0].Rune == '\b' || tokens[0].Rune == '\t'
	if control {
		advance = utf8.RuneLen(tokens[0].Rune)
	}
	if !control && len(data) > 1 && data[0] < utf8.RuneSelf && data[1] < utf8.RuneSelf {
		advance = 1
	}
	for advance == 0 {
		var err error
		advance, _, err = graphemes.SplitFunc(data, r.eof)
		if err != nil {
			return Cluster{}, err
		}
		// Keep CR/LF/BS/TAB separate for explicit pager row semantics.
		if advance > 0 {
			break
		}
		if len(data) >= MaxClusterBytes+utf8.UTFMax {
			return Cluster{}, fmt.Errorf("grapheme cluster exceeds %d-byte safety limit", MaxClusterBytes)
		}
		if err := r.fill(min(MaxClusterBytes+utf8.UTFMax, max(512, len(data)*2))); err != nil {
			return Cluster{}, err
		}
		data, tokens = r.data[r.dataStart:], r.tokens[r.tokenStart:]
	}
	if advance > MaxClusterBytes {
		return Cluster{}, fmt.Errorf("grapheme cluster exceeds %d-byte safety limit", MaxClusterBytes)
	}
	end := r.consumed + int64(advance)
	count := 1
	if advance > utf8.RuneLen(tokens[0].Rune) {
		ends := r.ends[r.tokenStart:]
		count = sort.Search(len(ends), func(i int) bool { return ends[i] >= end }) + 1
	}
	token := tokens[0]
	token.End = tokens[count-1].End
	after := tokens[count-1].Style
	afterUnderline := tokens[count-1].UnderlineColor
	last := count == len(tokens) && r.eof
	if last && !control {
		token.End, after = r.lastEnd, r.lastStyle
		afterUnderline = r.lastUnderlineColor
	}
	cluster := Cluster{Token: token, Text: string(data[:advance]), AfterStyle: after, AfterUnderlineColor: afterUnderline, Open: last && !r.complete && !control}
	r.dataStart += advance
	r.tokenStart += count
	r.consumed, r.emittedEnd = end, token.End
	return cluster, nil
}
