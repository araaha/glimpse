package ui

import (
	"testing"

	"github.com/gdamore/tcell/v3"
	"github.com/gdamore/tcell/v3/vt"
)

func TestRenderGhostCompletionAfterPrompt(t *testing.T) {
	screen, err := tcell.NewTerminfoScreenFromTty(vt.NewMockTerm(vt.MockOptSize{X: 12, Y: 2}), tcell.OptNegotiation(false))
	if err != nil {
		t.Fatal(err)
	}
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()
	frame := Frame{
		Layout: Layout{Body: Viewport{Width: 12, Height: 1}, StatusRow: -1, SearchRow: 1},
		Search: ":set hl", SearchActive: true, SearchCursor: 7, Ghost: "search",
	}
	if err := NewTerminal(screen).Render(frame); err != nil {
		t.Fatal(err)
	}
	var row string
	for x := range 12 {
		text, _, _ := screen.Get(x, 1)
		row += text
	}
	if row != ":set hlsearc" {
		t.Fatalf("row = %q", row)
	}
	if _, style, _ := screen.Get(6, 1); style.HasDim() {
		t.Fatal("typed text dimmed")
	}
	if _, style, _ := screen.Get(7, 1); !style.HasDim() {
		t.Fatal("ghost text not dimmed")
	}
}
