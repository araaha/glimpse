package ui

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/araaha/glimpse/internal/pager"

	"github.com/gdamore/tcell/v3"
	"github.com/gdamore/tcell/v3/color"
	"github.com/gdamore/tcell/v3/vt"
)

type colorOutputTerm struct {
	vt.MockTerm
	output bytes.Buffer
}

func (t *colorOutputTerm) Write(p []byte) (int, error) {
	t.output.Write(p)
	return t.MockTerm.Write(p)
}

func TestColorsSurviveCheckpointsAndReachTerminalOutput(t *testing.T) {
	t.Setenv("COLORTERM", "truecolor")
	t.Setenv("NO_COLOR", "")
	t.Setenv("TCELL_TRUECOLOR", "")
	// The git-log sample uses normal yellow/red/green/cyan, bold, red
	// backgrounds, and resets. Include explicit bright/indexed/RGB colors too.
	codes := []string{"33", "31", "32", "36", "1;33", "41", "91", "38;5;1", "38;5;196", "38;2;128;0;0", "38;2;255;255;255", "0"}
	foregrounds := []color.Color{color.Olive, color.Maroon, color.Green, color.Teal, color.Olive, color.Default, color.Red, color.Maroon, color.PaletteColor(196), color.NewHexColor(0x800000), color.White.TrueColor(), color.Default}
	emitted := []string{"33", "31", "32", "36", "33", "41", "38;5;9", "31", "38;5;196", "38;2;128;0;0", "38;2;255;255;255", ""}
	var input strings.Builder
	for _, code := range codes {
		fmt.Fprintf(&input, "\x1b[0;%sm%s\n", code, strings.Repeat("x", 128))
	}
	doc := pager.NewDocumentWithChunkSize(7)
	defer doc.Close()
	doc.Write([]byte(input.String()))
	text, err := NewTextWithLimits(doc, 16<<10, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer text.Close()
	// Enough rows to spill checkpoints; reflow must retain palette tags.
	for _, width := range []int{1, 16} {
		if err := text.Update(width, true); err != nil {
			t.Fatal(err)
		}
	}
	tty := &colorOutputTerm{MockTerm: vt.NewMockTerm(vt.MockOptSize{X: 16, Y: 2}, vt.MockOptNoBlit{})}
	screen, err := tcell.NewTerminfoScreenFromTty(tty, tcell.OptTerm("xterm-256color"), tcell.OptNegotiation(false))
	if err != nil {
		t.Fatal(err)
	}
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()
	for i, want := range foregrounds {
		tty.output.Reset()
		lines, err := text.Lines(i*8+3, 1) // Render from a wrapped checkpoint.
		if err != nil {
			t.Fatal(err)
		}
		layout := Layout{Body: Viewport{Width: 16, Height: 2}, StatusRow: -1, SearchRow: -1}
		if err := NewTerminal(screen).Render(Frame{Layout: layout, Lines: lines, Styled: text.Styled()}); err != nil {
			t.Fatal(err)
		}
		if err := tty.Drain(); err != nil {
			t.Fatal(err)
		}
		cell := tty.GetCell(vt.Coord{X: 0, Y: 0})
		_, style, _ := screen.Get(0, 0)
		if style.GetForeground() != want {
			t.Fatalf("SGR %s cell foreground %v, want %v", codes[i], style.GetForeground(), want)
		}
		if sequence := "\x1b[" + emitted[i] + "m"; !strings.Contains(tty.output.String(), sequence) {
			t.Fatalf("SGR %s missing emitted %q in %q", codes[i], sequence, tty.output.String())
		}
		// The emulator resolves default colors to its silver/black theme.
		if want == color.Default {
			want = color.Silver
		}
		// Assert RGB at the byte level above: the mock emulator does not
		// currently decode tcell's semicolon RGB output correctly.
		if cell.C != "x" || !want.IsRGB() && cell.S.Fg() != want {
			t.Fatalf("SGR %s emitted text %q foreground %v, want %v; bytes: %q", codes[i], cell.C, cell.S.Fg(), want, tty.output.String())
		}
		bg := color.Black
		if codes[i] == "41" {
			bg = color.Maroon
		}
		if cell.S.Bg() != bg {
			t.Fatalf("SGR %s emitted background %v, want %v", codes[i], cell.S.Bg(), bg)
		}
	}
}
