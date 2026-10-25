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
