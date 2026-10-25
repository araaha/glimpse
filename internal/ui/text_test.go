package ui

import (
	"reflect"
	"strings"
	"testing"

	"glimpse/internal/pager"
)

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
