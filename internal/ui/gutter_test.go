package ui

import (
	"testing"

	"github.com/araaha/glimpse/internal/pager"

	"github.com/gdamore/tcell/v3"
	"github.com/gdamore/tcell/v3/color"
	"github.com/gdamore/tcell/v3/vt"
)

func TestLineNumberAt(t *testing.T) {
	doc := pager.NewDocument()
	doc.Write([]byte("abcdef\nx\n"))
	text := NewText(doc)
	defer text.Close()
	if err := text.Update(4, true); err != nil {
		t.Fatal(err)
	}
	for row, want := range []struct {
		line  int
		first bool
	}{{1, true}, {1, false}, {2, true}, {0, false}} {
		if line, first := text.LineNumberAt(row); line != want.line || first != want.first {
			t.Fatalf("row %d = (%d, %v), want %+v", row, line, first, want)
		}
	}
}

func TestRenderGutterOffsetsTextAndHighlights(t *testing.T) {
	screen, err := tcell.NewTerminfoScreenFromTty(vt.NewMockTerm(vt.MockOptSize{X: 10, Y: 3}), tcell.OptNegotiation(false))
	if err != nil {
		t.Fatal(err)
	}
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()
	numbers := tcell.StyleDefault.Foreground(color.PaletteColor(8))
	frame := Frame{
		Layout: Layout{Body: Viewport{Width: 10, Height: 2}, StatusRow: -1, SearchRow: -1},
		Lines:  []string{"hello", "wrap"}, Styled: [][]Glyph{{{Text: "h", Column: 0, Width: 1}, {Text: "i", Column: 5, Width: 1}}},
		Gutter: []string{"12345", ""}, GutterWidth: 4, GutterStyle: numbers,
		Highlights: []Highlight{{Row: 0, Column: 0, Width: 1}},
	}
	if err := NewTerminal(screen).Render(frame); err != nil {
		t.Fatal(err)
	}
	row := func(y int) string {
		var out string
		for x := range 10 {
			text, _, _ := screen.Get(x, y)
			out += text
		}
		return out
	}
	// An over-wide number keeps its last digits; text starts after the gutter.
	if got := row(0); got != "345 h    i" {
		t.Fatalf("row 0 = %q", got)
	}
	if got := row(1); got != "    wrap  " {
		t.Fatalf("row 1 = %q", got)
	}
	if _, style, _ := screen.Get(0, 0); style.GetForeground() != color.PaletteColor(8) {
		t.Fatal("gutter style lost")
	}
	if _, style, _ := screen.Get(4, 0); !style.HasReverse() {
		t.Fatal("highlight not shifted by the gutter")
	}
	if text, _, _ := screen.Get(9, 0); text != "i" {
		t.Fatalf("last text column = %q", text)
	}
}
