package ui

import (
	"glimpse/internal/pager"
	"glimpse/internal/search"
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
	marks := text.Highlights(lines, 0, search.Query{Text: "界cd", SmartCase: true})
	if len(marks) != 3 || marks[0] != (Highlight{0, 2, 2}) || marks[1] != (Highlight{1, 0, 1}) {
		t.Fatalf("marks = %+v", marks)
	}
	if got := text.Highlights(lines, 0, search.Query{Text: "CDef"}); len(got) != 0 {
		t.Fatalf("matched across logical newline: %+v", got)
	}
}

func TestHighlightUsesRawMatchesAndIncludesOverlaps(t *testing.T) {
	for _, tt := range []struct {
		input, query string
		cells        int
	}{
		{"a\tb", "a       b", 0}, {"a\rb", "ab", 0}, {"a\x01b", "a�b", 0},
		{"banana", "ana", 5}, {"aaaa", "aaa", 4}, {"a\tb", "\t", 7},
	} {
		doc := pager.NewDocument()
		doc.Write([]byte(tt.input))
		text := NewText(doc)
		if err := text.Update(20, true); err != nil {
			t.Fatal(err)
		}
		lines, _ := text.Lines(0, 3)
		if got := text.Highlights(lines, 0, search.Query{Text: tt.query}); len(got) != tt.cells {
			t.Fatalf("%q / %q: %+v, want %d cells", tt.input, tt.query, got, tt.cells)
		}
	}
}

func TestHighlightRegexUsesWholeLines(t *testing.T) {
	doc := pager.NewDocument()
	doc.Write([]byte("xxxxxxyyzz\nxy\n"))
	text := NewText(doc)
	if err := text.Update(4, true); err != nil {
		t.Fatal(err)
	}
	lines, _ := text.Lines(1, 3) // "xxyy", "zz", "xy": the first line starts above
	for _, tt := range []struct {
		query string
		want  []Highlight
	}{
		{`^x+y+`, []Highlight{{0, 0, 1}, {0, 1, 1}, {0, 2, 1}, {0, 3, 1}, {2, 0, 1}, {2, 1, 1}}},
		{`y$`, []Highlight{{2, 1, 1}}},
		{`z$`, []Highlight{{1, 1, 1}}},
	} {
		got := text.Highlights(lines, 1, search.Query{Text: tt.query})
		if len(got) != len(tt.want) {
			t.Fatalf("%s: %+v, want %+v", tt.query, got, tt.want)
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Fatalf("%s: %+v, want %+v", tt.query, got, tt.want)
			}
		}
	}
}
