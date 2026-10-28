package ui

import (
	"reflect"
	"strings"
	"testing"

	"glimpse/internal/pager"
)

func unwrappedText(t *testing.T, input string, width int) (*Text, *countedTextSource) {
	t.Helper()
	source := &countedTextSource{Document: pager.NewDocument()}
	source.Write([]byte(input))
	text := NewText(source)
	text.SetWrap(false)
	if err := text.Update(width, true); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { text.Close(); source.Close() })
	return text, source
}

func TestUnwrappedRowsAreLogicalLines(t *testing.T) {
	text, _ := unwrappedText(t, "0123456789abc\nshort\n\x1b[31mred\x1b[0m line that is long\n", 4)
	if text.RowCount() != 3 || text.LineCount() != 3 || text.LineRow(3) != 2 {
		t.Fatalf("rows %d lines %d", text.RowCount(), text.LineCount())
	}
	for _, tt := range []struct {
		left     int
		want     []string
		overflow bool
	}{
		{0, []string{"0123", "shor", "red "}, true},
		{4, []string{"4567", "t", "line"}, true},
		{12, []string{"c", "", "t is"}, true},
		{30, []string{"", "", ""}, false},
	} {
		text.SetLeft(tt.left)
		lines, err := text.Lines(0, 3)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(lines, tt.want) || text.Overflow() != tt.overflow {
			t.Fatalf("left %d: %q overflow %v", tt.left, lines, text.Overflow())
		}
		for _, glyph := range text.Styled()[0] {
			if glyph.Column < 0 || glyph.Column >= 4 {
				t.Fatalf("glyph outside window: %+v", glyph)
			}
		}
	}
	// Styles before the window still apply inside it.
	text.SetLeft(1)
	if _, err := text.Lines(2, 1); err != nil {
		t.Fatal(err)
	}
	if glyph := text.Styled()[0][0]; glyph.Text != "e" || glyph.Style.ForegroundColor() == 0 {
		t.Fatalf("styled glyph = %+v", glyph)
	}
}

func TestUnwrappedWindowEdges(t *testing.T) {
	// A wide glyph cut by the left edge shows as a blank; tabs and CR use
	// whole-row columns.
	text, _ := unwrappedText(t, "a界bc\nx\ty\nabcdef\rZ\n", 3)
	text.SetLeft(2)
	lines, err := text.Lines(0, 3)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{" bc", "   ", "cde"}; !reflect.DeepEqual(lines, want) {
		t.Fatalf("edges = %q, want %q", lines, want)
	}
	for offset, wantColumn := range map[int64]int{0: 0, 1: 1, 4: 3, 9: 8, 14: 3, 18: 0} {
		if got := text.ColumnAt(offset); got != wantColumn {
			t.Fatalf("ColumnAt(%d) = %d, want %d", offset, got, wantColumn)
		}
	}
}

func TestUnwrappedResizeKeepsIndexAndToggleReindexes(t *testing.T) {
	text, _ := unwrappedText(t, strings.Repeat("x", 100)+"\ny\n", 10)
	before := text.Indexed()
	if text.NeedsUpdate(10, true) {
		t.Fatal("complete index needs update")
	}
	if !text.NeedsUpdate(20, true) {
		t.Fatal("width change must refresh the render width")
	}
	if err := text.Update(20, true); err != nil {
		t.Fatal(err)
	}
	if text.RowCount() != 2 || text.Indexed() != before {
		t.Fatalf("resize reindexed unwrapped rows: %d rows", text.RowCount())
	}
	if lines, _ := text.Lines(0, 1); lines[0] != strings.Repeat("x", 20) {
		t.Fatalf("render width not updated: %q", lines)
	}
	text.SetWrap(true)
	if err := text.Update(20, true); err != nil {
		t.Fatal(err)
	}
	if text.RowCount() != 6 {
		t.Fatalf("wrapped rows = %d", text.RowCount())
	}
}

func TestUnwrappedRenderingReadsOnlyThePrefix(t *testing.T) {
	for _, line := range []string{strings.Repeat("a", 1<<20), strings.Repeat("é", 1<<19), strings.Repeat("\x1b[1ma", 1<<17)} {
		text, source := unwrappedText(t, line+"\nnext\n", 8)
		text.SetLeft(16)
		source.readBytes = 0
		lines, err := text.Lines(0, 2)
		if err != nil || len(lines) != 2 || lines[1] != "" || !text.Overflow() {
			t.Fatalf("lines = %q, %v", lines, err)
		}
		text.Highlights(lines, 0, "a", false, false)
		if source.readBytes > 256<<10 {
			t.Fatalf("read %d bytes for a 24-column window", source.readBytes)
		}
	}
}
