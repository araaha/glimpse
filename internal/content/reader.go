package content

import (
	"bufio"
	"errors"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"
)

var ErrIncomplete = errors.New("incomplete content token")

type Token struct {
	Rune       rune
	Raw        rune
	Start, End int64
	Style      Style
	Hidden     bool
}

// Reader limits control sequences to 4096 bytes. Malformed or oversized input
// falls back to visible replacement text; it can never reach the real terminal
// as an escape. Incomplete UTF-8/escapes wait only while the source is still live.
type Reader struct {
	reader   *bufio.Reader
	offset   int64
	style    Style
	complete bool
}

func NewReader(source io.ReaderAt, start, end int64, style Style, complete bool) *Reader {
	return &Reader{reader: bufio.NewReaderSize(io.NewSectionReader(source, start, end-start), 4096), offset: start, style: style, complete: complete}
}

func (r *Reader) Next() (Token, error) {
	start := r.offset
	peek, err := r.reader.Peek(1)
	if err != nil {
		return Token{}, err
	}
	if peek[0] >= utf8.RuneSelf && !r.complete {
		prefix, _ := r.reader.Peek(utf8.UTFMax)
		if !utf8.FullRune(prefix) {
			return Token{}, ErrIncomplete
		}
	}
	ch, size, err := r.reader.ReadRune()
	if err != nil {
		return Token{}, err
	}
	r.offset += int64(size)
	if ch == 0x1b || ch == 0x9b || ch == 0x9d || ch == 0x90 || ch == 0x9e || ch == 0x9f {
		token, err := r.escape(ch, start)
		if err != nil {
			return Token{}, err
		}
		return token, nil
	}
	raw := ch
	if unicode.IsControl(ch) && ch != '\n' && ch != '\r' && ch != '\b' && ch != '\t' {
		ch = utf8.RuneError
	}
	return Token{Rune: ch, Raw: raw, Start: start, End: r.offset, Style: r.style}, nil
}

func (r *Reader) escape(ch rune, start int64) (Token, error) {
	introducer := byte('[')
	if ch == 0x1b {
		next, err := r.reader.ReadByte()
		if err != nil {
			return r.incomplete(start, err)
		}
		r.offset++
		introducer = next
	} else {
		switch ch {
		case 0x9d:
			introducer = ']'
		case 0x90:
			introducer = 'P'
		case 0x9e:
			introducer = '^'
		case 0x9f:
			introducer = '_'
		}
	}
	hidden := func() (Token, error) { return Token{Start: start, End: r.offset, Style: r.style, Hidden: true}, nil }
	if introducer == '[' {
		var parameters strings.Builder
		for count := 0; count < 4096; count++ {
			b, err := r.reader.ReadByte()
			if err != nil {
				return r.incomplete(start, err)
			}
			r.offset++
			if b >= 0x40 && b <= 0x7e {
				if b == 'm' && parameters.Len() <= 512 {
					r.style = sgr(r.style, parameters.String())
				}
				return hidden()
			}
			if b < 0x20 || b > 0x3f {
				return Token{Rune: utf8.RuneError, Start: start, End: r.offset, Style: r.style}, nil
			}
			if parameters.Len() < 513 {
				parameters.WriteByte(b)
			}
		}
		return Token{Rune: utf8.RuneError, Start: start, End: r.offset, Style: r.style}, nil
	}
	if introducer == ']' || introducer == 'P' || introducer == '^' || introducer == '_' || introducer == 'X' {
		escape := false
		c1 := false
		for count := 0; count < 4096; count++ {
			b, err := r.reader.ReadByte()
			if err != nil {
				return r.incomplete(start, err)
			}
			r.offset++
			if b == 7 || escape && b == '\\' || c1 && b == 0x9c {
				return hidden()
			}
			escape = b == 0x1b
			c1 = b == 0xc2
		}
		return Token{Rune: utf8.RuneError, Start: start, End: r.offset, Style: r.style}, nil
	}
	if introducer >= 0x20 && introducer <= 0x2f {
		for range 16 {
			b, err := r.reader.ReadByte()
			if err != nil {
				return r.incomplete(start, err)
			}
			r.offset++
			if b >= 0x30 && b <= 0x7e {
				return hidden()
			}
		}
	}
	return hidden()
}

func (r *Reader) incomplete(start int64, err error) (Token, error) {
	if !errors.Is(err, io.EOF) {
		return Token{}, err
	}
	if !r.complete {
		return Token{}, ErrIncomplete
	}
	return Token{Rune: utf8.RuneError, Start: start, End: r.offset, Style: r.style}, nil
}
