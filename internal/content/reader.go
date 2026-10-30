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
	Rune           rune
	Raw            rune
	Start, End     int64
	Style          Style
	Hidden         bool
	UnderlineColor Color
	// Unprintable marks a replacement for a control character, DEL, an
	// invalid UTF-8 byte, or a malformed escape (not a literal U+FFFD).
	Unprintable bool
}

type ASCIIRun struct {
	Bytes          []byte
	Start          int64
	Style          Style
	UnderlineColor Color
}

func asciiPrefix(data []byte) int {
	n := 0
	for n < len(data) && (data[n] >= ' ' && data[n] <= '~' || data[n] == '\n') {
		n++
	}
	// The final printable rune may acquire a combining suffix or joiner,
	// including across hidden styling or the next published input chunk.
	if n > 0 && data[n-1] != '\n' {
		n--
	}
	return n
}

// plainPrefix is asciiPrefix for decoders of runes rather than graphemes, for
// which tabs are ordinary text. The final byte is still held back: a later
// backspace, even past hidden controls, may overstrike it.
func plainPrefix(data []byte) int {
	n := 0
	for n < len(data) && (data[n] >= ' ' && data[n] <= '~' || data[n] == '\n' || data[n] == '\t') {
		n++
	}
	if n > 0 && data[n-1] != '\n' {
		n--
	}
	return n
}

// sgrPrefix recognizes a complete buffered SGR without consuming it. Other
// controls, incomplete sequences, and Unicode go through the general decoder.
func sgrPrefix(data []byte) int {
	if len(data) < 3 || data[0] != 0x1b || data[1] != '[' {
		return 0
	}
	for i := 2; i < len(data) && i < 4098; i++ {
		b := data[i]
		if b >= 0x40 && b <= 0x7e {
			if b == 'm' {
				return i + 1
			}
			return 0
		}
		if b < 0x20 || b > 0x3f {
			return 0
		}
	}
	return 0
}

func asciiBoundary(data []byte) bool {
	for len(data) > 0 {
		b := data[0]
		if b >= ' ' && b <= '~' || b == '\n' || b == '\r' || b == '\t' {
			return true
		}
		n := sgrPrefix(data)
		if n == 0 {
			break
		}
		data = data[n:]
	}
	return false
}

func (r *Reader) ascii() (ASCIIRun, bool) {
	if r.reader.Buffered() == 0 {
		if _, err := r.reader.Peek(1); err != nil {
			return ASCIIRun{}, false
		}
	}
	if r.reader.Buffered() == 1 {
		r.reader.Peek(2)
	}
	data, _ := r.reader.Peek(r.reader.Buffered())
	var n int
	if r.ignoreStyles {
		n = plainPrefix(data)
		if n < len(data) && (data[n] >= ' ' && data[n] <= '~' || data[n] == '\t') && asciiBoundary(data[n+1:]) {
			n++ // Only SGR separates it from text, so no backspace can follow.
		}
	} else {
		n = asciiPrefix(data)
		if n < len(data) && data[n] >= ' ' && data[n] <= '~' && asciiBoundary(data[n+1:]) {
			n++ // A buffered SGR followed by ASCII cannot extend this grapheme.
		}
	}
	if n == 0 {
		return ASCIIRun{}, false
	}
	run := ASCIIRun{Bytes: data[:n], Start: r.offset, Style: r.style, UnderlineColor: r.underlineColor}
	r.reader.Discard(n)
	r.offset += int64(n)
	return run, true
}

// SkipSGR decodes one complete SGR sequence at the start of the buffered input,
// without reading further. It reports false when the input starts otherwise.
func (r *Reader) SkipSGR() (Token, bool) {
	if r.reader.Buffered() == 0 {
		r.reader.Peek(1)
	}
	data, _ := r.reader.Peek(r.reader.Buffered())
	if sgrPrefix(data) == 0 {
		return Token{}, false
	}
	token, err := r.Next()
	return token, err == nil
}

// ASCII returns a contiguous run without decoding individual tokens. Buffered
// lookahead must prove the last byte cannot join a grapheme or an overstrike;
// a plain reader decodes runes, so its runs also include tabs.
// Bytes remains valid only until the next reader operation.
func (r *Reader) ASCII() (ASCIIRun, bool) { return r.ascii() }

// Reader limits control sequences to 4096 bytes. Malformed or oversized input
// falls back to visible replacement text; it can never reach the real terminal
// as an escape. Incomplete UTF-8/escapes wait only while the source is still live.
type Reader struct {
	reader         *bufio.Reader
	offset         int64
	style          Style
	complete       bool
	underlineColor Color
	ignoreStyles   bool
}

// NewPlainReader preserves token boundaries and safe control suppression but
// skips SGR style parsing, for consumers interested only in searchable text.
func NewPlainReader(source io.ReaderAt, start, end int64) *Reader {
	r := NewReader(source, start, end, 0, true)
	r.ignoreStyles = true
	return r
}

func NewReader(source io.ReaderAt, start, end int64, style Style, complete bool, underline ...Color) *Reader {
	// Long scans, such as indexing, read in large blocks. Rows and nearby searches
	// read only what they need, which a large buffer would overshoot.
	size := 4 << 10
	if end-start >= 1<<20 {
		size = 64 << 10
	}
	r := &Reader{reader: bufio.NewReaderSize(io.NewSectionReader(source, start, end-start), size), offset: start, style: style, complete: complete}
	if len(underline) > 0 {
		r.underlineColor = underline[0]
	}
	return r
}

// textStops are the bytes that can begin a newline or a control sequence:
// '\n', ESC, and 0xC2, the UTF-8 lead byte of the C1 introducers. Other bytes
// decode to runes that can neither be nor hide a newline.
var textStops = [256]bool{'\n': true, 0x1b: true, 0xc2: true}

// SkipText discards input up to the next byte in textStops, or to the end,
// without decoding it. Next then decodes from that byte exactly as it would
// have without the skip, because no UTF-8 sequence spans one of those bytes.
func (r *Reader) SkipText() error {
	for {
		if r.reader.Buffered() == 0 {
			if _, err := r.reader.Peek(1); err != nil {
				return err
			}
		}
		data, _ := r.reader.Peek(r.reader.Buffered())
		n := 0
		for n < len(data) && !textStops[data[n]] {
			n++
		}
		r.reader.Discard(n)
		r.offset += int64(n)
		if n < len(data) {
			return nil
		}
	}
}

func (r *Reader) Next() (Token, error) {
	start := r.offset
	peek, err := r.reader.Peek(1)
	if err != nil {
		return Token{}, err
	}
	if peek[0] == 0x1b {
		data, _ := r.reader.Peek(r.reader.Buffered())
		if n := sgrPrefix(data); n > 0 {
			if !r.ignoreStyles && n-3 <= 512 {
				r.style, r.underlineColor = sgrBytes(r.style, r.underlineColor, data[2:n-1])
			}
			r.reader.Discard(n)
			r.offset += int64(n)
			return Token{Start: start, End: r.offset, Style: r.style, Hidden: true, UnderlineColor: r.underlineColor}, nil
		}
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
	if ch == 0x1b || ch == 0x9b || ch == 0x9d || ch == 0x90 || ch == 0x98 || ch == 0x9e || ch == 0x9f {
		token, err := r.escape(ch, start)
		if err != nil {
			return Token{}, err
		}
		return token, nil
	}
	raw, unprintable := ch, ch == utf8.RuneError && size == 1
	if unicode.IsControl(ch) && ch != '\n' && ch != '\r' && ch != '\b' && ch != '\t' {
		ch, unprintable = utf8.RuneError, true
	}
	return Token{Rune: ch, Raw: raw, Unprintable: unprintable, Start: start, End: r.offset, Style: r.style, UnderlineColor: r.underlineColor}, nil
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
		case 0x98:
			introducer = 'X'
		case 0x9e:
			introducer = '^'
		case 0x9f:
			introducer = '_'
		}
	}
	hidden := func() (Token, error) {
		return Token{Start: start, End: r.offset, Style: r.style, Hidden: true, UnderlineColor: r.underlineColor}, nil
	}
	if introducer == '[' {
		var parameters strings.Builder
		for count := 0; count < 4096; count++ {
			b, err := r.reader.ReadByte()
			if err != nil {
				return r.incomplete(start, err)
			}
			r.offset++
			if b >= 0x40 && b <= 0x7e {
				if !r.ignoreStyles && b == 'm' && parameters.Len() <= 512 {
					r.style, r.underlineColor = sgr(r.style, r.underlineColor, parameters.String())
				}
				return hidden()
			}
			if b < 0x20 || b > 0x3f {
				return Token{Rune: utf8.RuneError, Unprintable: true, Start: start, End: r.offset, Style: r.style, UnderlineColor: r.underlineColor}, nil
			}
			if !r.ignoreStyles && parameters.Len() < 513 {
				parameters.WriteByte(b)
			}
		}
		return Token{Rune: utf8.RuneError, Unprintable: true, Start: start, End: r.offset, Style: r.style, UnderlineColor: r.underlineColor}, nil
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
			if introducer == ']' && b == 7 || escape && b == '\\' || c1 && b == 0x9c {
				return hidden()
			}
			escape = b == 0x1b
			c1 = b == 0xc2
		}
		return Token{Rune: utf8.RuneError, Unprintable: true, Start: start, End: r.offset, Style: r.style, UnderlineColor: r.underlineColor}, nil
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
	return Token{Rune: utf8.RuneError, Unprintable: true, Start: start, End: r.offset, Style: r.style, UnderlineColor: r.underlineColor}, nil
}
