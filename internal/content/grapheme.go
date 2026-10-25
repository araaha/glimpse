package content

import (
	"errors"
	"fmt"
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
	Text       string
	AfterStyle Style
	Open       bool // Last live cluster may grow when more input arrives.
}

// GraphemeReader batches decoded tokens, then uses the Unicode UAX #29 segmenter
// on progressively larger buffers. This avoids rescanning long combining runs
// once per rune. Source mappings and buffers remain bounded by MaxClusterBytes.
type GraphemeReader struct {
	reader              *Reader
	data                []byte
	tokens              []Token
	ends                []int64
	encoded, consumed   int64
	lastEnd, emittedEnd int64
	lastStyle           Style
	eof, complete       bool
}

func NewGraphemeReader(source io.ReaderAt, start, end int64, style Style, complete bool) *GraphemeReader {
	return &GraphemeReader{reader: NewReader(source, start, end, style, complete), complete: complete, lastEnd: start, emittedEnd: start, lastStyle: style}
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
		if token.Hidden {
			continue
		}
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
			token := Token{Start: r.emittedEnd, End: r.lastEnd, Style: r.lastStyle, Hidden: true}
			r.emittedEnd = r.lastEnd
			return Cluster{Token: token, AfterStyle: r.lastStyle}, nil
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
	last := count == len(r.tokens) && r.eof
	if last {
		token.End, after = r.lastEnd, r.lastStyle
	}
	cluster := Cluster{Token: token, Text: string(r.data[:advance]), AfterStyle: after, Open: last && !r.complete && !control}
	r.data, r.tokens, r.ends = r.data[advance:], r.tokens[count:], r.ends[count:]
	r.consumed, r.emittedEnd = end, token.End
	return cluster, nil
}
