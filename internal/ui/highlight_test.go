package ui

import (
	"glimpse/internal/pager"
	"testing"
)

func TestHighlightWrappedMatches(t *testing.T) {
	doc := pager.NewDocument()
	doc.Write([]byte("ab界CD\nefgh"))
	text := NewText(doc)
	if err := text.Update(4, true); err != nil {
		t.Fatal(err)
	}
	lines, _ := text.Lines(0, 10)
	marks := text.Highlights(lines, 0, "界cd", true, false)
	if len(marks) != 3 || marks[0] != (Highlight{0, 2, 2}) || marks[1] != (Highlight{1, 0, 1}) {
		t.Fatalf("marks = %+v", marks)
	}
	if got := text.Highlights(lines, 0, "CDef", false, false); len(got) != 0 {
		t.Fatalf("matched across logical newline: %+v", got)
	}
}

func TestHighlightUsesRawMatchesAndIncludesOverlaps(t *testing.T) {
	for _, tt := range []struct {
		input, query string
		cells        int
	}{
		{"a\tb", "a       b", 0}, {"a\rb", "ab", 0}, {"a\x01b", "a�b", 0},
		{"banana", "ana", 5}, {"aaaa", "aaa", 4}, {"a\tb", "\t", 1},
	} {
		doc := pager.NewDocument()
		doc.Write([]byte(tt.input))
		text := NewText(doc)
		if err := text.Update(20, true); err != nil {
			t.Fatal(err)
		}
		lines, _ := text.Lines(0, 3)
		if got := text.Highlights(lines, 0, tt.query, false, false); len(got) != tt.cells {
			t.Fatalf("%q / %q: %+v, want %d cells", tt.input, tt.query, got, tt.cells)
		}
	}
}
