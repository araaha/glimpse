package ui

import (
	"testing"

	"glimpse/internal/pager"

	"github.com/gdamore/tcell/v3"
	"github.com/gdamore/tcell/v3/color"
	"github.com/gdamore/tcell/v3/vt"
)

func TestUnprintableGlyphs(t *testing.T) {
	doc := pager.NewDocument()
	// NUL, BEL, DEL, C1 NEL, an invalid byte, a malformed escape, and a
	// literal U+FFFD, which is printable text.
	doc.Write([]byte("a\x00b\x07c\x7fd\u0085e\xffg\x1b[1\x01h�i\n"))
	text := NewText(doc)
	defer text.Close()
	text.SetUnprintable("·")
	if err := text.Update(80, true); err != nil {
		t.Fatal(err)
	}
	lines, err := text.Lines(0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if want := "a·b·c·d·e·g·h�i"; lines[0] != want {
		t.Fatalf("line = %q, want %q", lines[0], want)
	}
	for _, glyph := range text.Styled()[0] {
		if glyph.Unprintable != (glyph.Text == "·") {
			t.Fatalf("glyph %+v has the wrong unprintable mark", glyph)
		}
	}
	// Without a configured character the replacement stays U+FFFD.
	text.SetUnprintable("")
	if lines, _ := text.Lines(0, 1); lines[0] != "a�b�c�d�e�g�h�i" {
		t.Fatalf("default = %q", lines[0])
	}
}

func TestRenderUnprintableColors(t *testing.T) {
	screen, err := tcell.NewTerminfoScreenFromTty(vt.NewMockTerm(vt.MockOptSize{X: 4, Y: 1}), tcell.OptNegotiation(false))
	if err != nil {
		t.Fatal(err)
	}
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()
	frame := Frame{
		Layout:           Layout{Body: Viewport{Width: 4, Height: 1}, StatusRow: -1, SearchRow: -1},
		Lines:            []string{"a·"},
		Styled:           [][]Glyph{{{Text: "a", Column: 0, Width: 1}, {Text: "·", Column: 1, Width: 1, Unprintable: true}}},
		UnprintableStyle: tcell.StyleDefault.Foreground(color.PaletteColor(1)),
	}
	if err := NewTerminal(screen).Render(frame); err != nil {
		t.Fatal(err)
	}
	if _, style, _ := screen.Get(1, 0); style.GetForeground() != color.PaletteColor(1) {
		t.Fatal("unprintable glyph not colored")
	}
	if _, style, _ := screen.Get(0, 0); style.GetForeground() == color.PaletteColor(1) {
		t.Fatal("printable glyph colored")
	}
}

func TestValidateCellText(t *testing.T) {
	for _, tt := range []struct {
		text string
		wide bool
		ok   bool
	}{{"?", false, true}, {"·", false, true}, {"·", true, false}, {"�", false, true}, {"界", false, false}, {"ab", false, false}, {"", false, false}, {"\x07", false, false}, {"é", false, true}} {
		if err := ValidateCellText(tt.text, tt.wide); (err == nil) != tt.ok {
			t.Fatalf("%q wide=%v: %v", tt.text, tt.wide, err)
		}
	}
}
