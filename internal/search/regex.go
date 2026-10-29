package search

import (
	"bytes"
	"errors"
	"io"
	"unicode/utf8"
)

// regexLine buffers the decoded text of one line, or a window of a very long
// one, with the source offsets of the token behind each byte.
type regexLine struct {
	text         []byte
	starts, ends []int64
	report       int // matches starting earlier were reported or are context
	lastEnd      int // end of the last reported match; the next window starts there
}

func (l *regexLine) add(text []byte, start, end int64) {
	l.text = append(l.text, text...)
	for range text {
		l.starts, l.ends = append(l.starts, start), append(l.ends, end)
	}
}

func (l *regexLine) addASCII(text []byte, offset int64) {
	l.text = append(l.text, text...)
	for i := range text {
		at := offset + int64(i)
		l.starts, l.ends = append(l.starts, at), append(l.ends, at+1)
	}
}

func (l *regexLine) reset() {
	l.text, l.starts, l.ends = l.text[:0], l.starts[:0], l.ends[:0]
	l.report, l.lastEnd = 0, 0
}

// scan visits the window's new matches. A final window ends the line; any
// other one reports only matches of at most MaxMatchBytes starting before its
// last MaxMatchBytes, which the next window keeps. Empty matches are skipped.
// It returns false once visit stops or matches pass lastStart.
func (l *regexLine) scan(m matcher, final bool, lastStart int64, visit func(start, end int64) bool) bool {
	text, limit := l.text, len(l.text)-MaxMatchBytes
	if final {
		text = bytes.TrimSuffix(text, []byte{'\r'})
		limit = len(text)
	}
	report := func(start, end int) (bool, bool) {
		if start >= limit {
			return false, true
		}
		if start == end || !final && end-start > MaxMatchBytes {
			return true, true
		}
		at := l.starts[start]
		if at > lastStart || !visit(at, l.ends[end-1]) {
			return false, false
		}
		l.lastEnd = end
		return true, true
	}
	if l.report == 0 {
		for _, match := range m.re.FindAllIndex(text, -1) {
			if more, ok := report(match[0], match[1]); !more {
				return ok
			}
		}
		return true
	}
	// A later window starts mid-line: resume after the rune before each
	// position, so that rune is context rather than a possible match start.
	for at := l.report; at < len(text); {
		before := at - 1
		for before > 0 && !utf8.RuneStart(text[before]) {
			before--
		}
		match := m.resume.FindSubmatchIndex(text[before:])
		if match == nil {
			return true
		}
		start, end := before+match[2], before+match[3]
		if more, ok := report(start, end); !more {
			return ok
		}
		if start == end {
			_, size := utf8.DecodeRune(text[start:])
			end += size
		}
		at = end
	}
	return true
}

// slide drops the window's reported text, keeping the rune before the rest as
// context so that ^ and \b do not match spuriously at the new start.
func (l *regexLine) slide() {
	from := max(len(l.text)-MaxMatchBytes, l.lastEnd)
	cut := from - 1
	for cut > 0 && !utf8.RuneStart(l.text[cut]) {
		cut--
	}
	l.report, l.lastEnd = from-cut, 0
	l.text = l.text[:copy(l.text, l.text[cut:])]
	l.starts = l.starts[:copy(l.starts, l.starts[cut:])]
	l.ends = l.ends[:copy(l.ends, l.ends[cut:])]
}

// walkRegex matches one decoded line at a time, so a match never spans a
// newline. Lines starting after lastStart are not decoded.
func walkRegex(reader *searchReader, m matcher, start, lastStart int64, visit func(start, end int64) bool) (straddled, open bool, err error) {
	var line regexLine
	var utf [utf8.UTFMax]byte
	// grown checks a long line's window; it returns false to stop the walk.
	grown := func() bool {
		if len(line.text) < 2*MaxMatchBytes {
			return true
		}
		if !line.scan(m, false, lastStart, visit) {
			return false
		}
		line.slide()
		return line.starts[line.report] <= lastStart
	}
	// newline ends the current line; next is where the following one starts.
	newline := func(next int64) bool {
		ok := line.scan(m, true, lastStart, visit)
		line.reset()
		return ok && next <= lastStart
	}
	if start > lastStart {
		return false, false, nil
	}
	for {
		if err := reader.ctx.Err(); err != nil {
			return false, false, err
		}
		if run, ok := reader.ascii(); ok {
			data, offset := run.Bytes, run.Start
			for len(data) > 0 {
				i := bytes.IndexByte(data, '\n')
				if i < 0 {
					line.addASCII(data, offset)
					break
				}
				line.addASCII(data[:i], offset)
				if !newline(offset + int64(i) + 1) {
					return reader.straddled, false, nil
				}
				data, offset = data[i+1:], offset+int64(i)+1
			}
			if !grown() {
				return reader.straddled, false, nil
			}
			continue
		}
		token, err := reader.next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return false, false, err
		}
		// Overstrike normalization can merge tokens across a hidden control.
		reader.check(token.Start, token.End)
		if token.Hidden {
			continue
		}
		if token.Raw == '\n' {
			if !newline(token.End) {
				return reader.straddled, false, nil
			}
			continue
		}
		line.add(utf[:utf8.EncodeRune(utf[:], token.Raw)], token.Start, token.End)
		if !grown() {
			return reader.straddled, false, nil
		}
	}
	if len(line.text) > 0 && !line.scan(m, true, lastStart, visit) {
		return reader.straddled, false, nil
	}
	return reader.straddled, true, nil
}
