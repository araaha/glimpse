package ui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"glimpse/internal/pager"
)

func TestTextIgnoredBytesDoNotHideVisibleCells(t *testing.T) {
	for _, prefixLength := range []int{255, 256, 257, 4095, 4096, 4097} {
		t.Run(fmt.Sprint(prefixLength), func(t *testing.T) {
			doc := pager.NewDocumentWithChunkSize(3)
			text := NewText(doc)
			for _, part := range []string{strings.Repeat("\r", prefixLength), "hel", "lo"} {
				if _, err := doc.Write([]byte(part)); err != nil {
					t.Fatal(err)
				}
				if err := text.Update(4, false); err != nil {
					t.Fatal(err)
				}
			}
			for _, width := range []int{4, 3, 8} {
				if err := text.Update(width, true); err != nil {
					t.Fatal(err)
				}
				got, err := text.Lines(0, 10)
				if err != nil {
					t.Fatal(err)
				}
				var want []string
				for rest := "hello"; len(rest) > 0; {
					n := min(width, len(rest))
					want = append(want, rest[:n])
					rest = rest[n:]
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("width %d: lines = %q, want %q", width, got, want)
				}
			}
		})
	}
}

func TestTextLongZeroWidthSpanKeepsVisibleSuffixBounded(t *testing.T) {
	doc := pager.NewDocument()
	doc.Write([]byte("a" + strings.Repeat("\u0301", 4096) + "bcde"))
	text := NewText(doc)
	if err := text.Update(4, true); err != nil {
		t.Fatal(err)
	}
	lines, err := text.Lines(0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 || len(lines[0]) > max(256, 4*16) {
		t.Fatalf("row output is not bounded: %d rows", len(lines))
	}
	visible := strings.ReplaceAll(lines[0], "\u0301", "")
	if visible != "abcd" || lines[1] != "e" {
		t.Fatalf("visible cells = %q, %q; want abcd, e", visible, lines[1])
	}
}

func TestTextUTF8AfterIgnoredPrefixCrossesRowReadBoundary(t *testing.T) {
	doc := pager.NewDocumentWithChunkSize(3)
	doc.Write([]byte(strings.Repeat("\r", 4095) + "界e\u0301x"))
	text := NewText(doc)
	if err := text.Update(4, true); err != nil {
		t.Fatal(err)
	}
	lines, err := text.Lines(0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(lines, []string{"界e\u0301x"}) {
		t.Fatalf("UTF-8 across the row buffer boundary = %q", lines)
	}
}

type countedTextSource struct {
	*pager.Document
	readBytes int
}

func (s *countedTextSource) ReadAt(p []byte, offset int64) (int, error) {
	n, err := s.Document.ReadAt(p, offset)
	s.readBytes += n
	return n, err
}

func TestTextFullRowDoesNotReadEntireInvisibleTail(t *testing.T) {
	source := &countedTextSource{Document: pager.NewDocument()}
	source.Write([]byte("abcd" + strings.Repeat("\r", 8192) + "\ne"))
	text := NewText(source)
	if err := text.Update(4, true); err != nil {
		t.Fatal(err)
	}
	source.readBytes = 0
	lines, err := text.Lines(0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(lines, []string{"abcd"}) {
		t.Fatalf("full row = %q", lines)
	}
	if source.readBytes > 4096 {
		t.Fatalf("read %d bytes after the visible row was already full", source.readBytes)
	}
}

func TestTextRows(t *testing.T) {
	tests := []struct {
		name  string
		input string
		width int
		want  []string
	}{
		{"empty", "", 4, nil},
		{"unfinished line", "abcdefghij", 4, []string{"abcd", "efgh", "ij"}},
		{"exact width", "abcdefgh", 4, []string{"abcd", "efgh"}},
		{"newline after exact width", "abcd\nx", 4, []string{"abcd", "x"}},
		{"trailing newline", "abc\n", 4, []string{"abc"}},
		{"empty lines", "a\n\nb\n", 4, []string{"a", "", "b"}},
		{"only newlines", "\n\n", 4, []string{"", ""}},
		{"CRLF", "a\r\nb", 4, []string{"a", "b"}},
		{"tabs", "a\tb", 10, []string{"a       b"}},
		{"tab at edge", "abc\tx", 4, []string{"abc ", "x"}},
		{"wide text", "a界bc", 4, []string{"a界b", "c"}},
		{"wide character wraps", "abc界", 4, []string{"abc", "界"}},
		{"one column wide text", "界x", 1, []string{"�", "x"}},
		{"combining text", "e\u0301x", 1, []string{"e\u0301", "x"}},
		{"escape is not executed", "\x1b[31m", 10, []string{"�[31m"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := pager.NewDocumentWithChunkSize(3)
			doc.Write([]byte(tt.input))
			text := NewText(doc)
			if err := text.Update(tt.width, true); err != nil {
				t.Fatal(err)
			}
			got, err := text.Lines(0, 100)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) || text.RowCount() != len(tt.want) {
				t.Fatalf("lines = %q, rows = %d, want %q", got, text.RowCount(), tt.want)
			}
		})
	}
}

func TestTextLineRow(t *testing.T) {
	for _, input := range []string{"", "abcdefgh\r\n\n界界界\nx", "abcdefgh\r\n\n界界界\nx\n"} {
		doc := pager.NewDocumentWithChunkSize(3)
		text := NewText(doc)
		// Incremental writes must also build the logical-line index correctly.
		for _, r := range input {
			doc.Write([]byte(string(r)))
			if err := text.Update(3, false); err != nil {
				t.Fatal(err)
			}
		}
		for _, width := range []int{3, 8, 1} {
			if err := text.Update(width, true); err != nil {
				t.Fatal(err)
			}
			offsets := []int64{0, 10, 11, 21}
			if input == "" {
				offsets = []int64{0}
			}
			for _, line := range []int{0, 1, 2, 3, 4, 50} {
				want := text.RowAt(offsets[min(max(line, 1), len(offsets))-1])
				if got := text.LineRow(line); got != want {
					t.Fatalf("input %q, width %d, line %d: row = %d, want %d", input, width, line, got, want)
				}
			}
		}
	}
}

func TestTextAppendsWithoutNewline(t *testing.T) {
	doc := pager.NewDocument()
	text := NewText(doc)
	for _, part := range []string{"a", "bcde", "fg", "h\n", "i"} {
		doc.Write([]byte(part))
		if err := text.Update(4, false); err != nil {
			t.Fatal(err)
		}
	}
	lines, _ := text.Lines(0, 10)
	if want := []string{"abcd", "efgh", "i"}; !reflect.DeepEqual(lines, want) {
		t.Fatalf("incremental lines = %q, want %q", lines, want)
	}
	if text.scanned != doc.Len() {
		t.Fatal("partial input was not indexed")
	}
}

func TestTextSplitUTF8(t *testing.T) {
	doc := pager.NewDocument()
	text := NewText(doc)
	data := []byte("界")
	doc.Write(data[:1])
	if err := text.Update(4, false); err != nil {
		t.Fatal(err)
	}
	if text.RowCount() != 0 {
		t.Fatal("incomplete UTF-8 rune was published as invalid")
	}
	doc.Write(data[1:])
	text.Update(4, false)
	lines, _ := text.Lines(0, 1)
	if !reflect.DeepEqual(lines, []string{"界"}) {
		t.Fatalf("split UTF-8 = %q", lines)
	}
}

func TestTextTruncatedUTF8AtEOF(t *testing.T) {
	doc := pager.NewDocument()
	doc.Write([]byte{0xe7})
	text := NewText(doc)
	text.Update(4, false)
	text.Update(4, true)
	lines, _ := text.Lines(0, 1)
	if !reflect.DeepEqual(lines, []string{"�"}) {
		t.Fatalf("truncated UTF-8 = %q", lines)
	}
}

func TestTextUTF8AcrossReadBoundary(t *testing.T) {
	doc := pager.NewDocument()
	doc.Write([]byte(strings.Repeat("a", 64*1024-1) + "界"))
	text := NewText(doc)
	if err := text.Update(80, true); err != nil {
		t.Fatal(err)
	}
	lines, _ := text.Lines(text.RowCount()-1, 1)
	if len(lines) != 1 || !strings.HasSuffix(lines[0], "界") {
		t.Fatalf("read boundary broke UTF-8: %q", lines)
	}
}

func TestTextResizePreservesByteAnchor(t *testing.T) {
	doc := pager.NewDocument()
	doc.Write([]byte("abcdefghijkl"))
	text := NewText(doc)
	text.Update(4, true)
	anchor := text.Offset(2)
	text.Update(3, true)
	if got := text.RowAt(anchor); got != 2 {
		t.Fatalf("RowAt(%d) = %d, want 2", anchor, got)
	}
	lines, _ := text.Lines(text.RowAt(anchor), 1)
	if !reflect.DeepEqual(lines, []string{"ghi"}) {
		t.Fatalf("resized anchor = %q", lines)
	}
}
