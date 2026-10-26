package ui

import (
	"os"
	"strings"
	"testing"

	"glimpse/internal/content"
	"glimpse/internal/pager"

	"github.com/gdamore/tcell/v3"
	"github.com/gdamore/tcell/v3/vt"
)

func TestMoorColoredUnderlineSample(t *testing.T) {
	input, err := os.ReadFile("testdata/colored-underlines.txt")
	if err != nil {
		t.Fatal(err)
	}
	for split := 0; split <= len(input); split++ {
		doc := pager.NewDocumentWithChunkSize(3)
		text, _ := NewTextWithLimits(doc, 16<<10, 1<<20)
		doc.Write(input[:split])
		if err := text.Update(9, false); err != nil {
			t.Fatal(err)
		}
		doc.Write(input[split:])
		if err := text.Update(9, true); err != nil {
			t.Fatal(err)
		}
		for _, width := range []int{9, 4, 80} {
			if err := text.Update(width, true); err != nil {
				t.Fatal(err)
			}
			lines, err := text.Lines(0, 100)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Join(lines, "") != "Red underline Default colored underline" {
				t.Fatalf("split %d width %d text = %q", split, width, lines)
			}
			column := 0
			for _, row := range text.Styled() {
				for _, glyph := range row {
					if glyph.Style&content.Underline == 0 {
						t.Fatalf("split %d lost underline at character %d", split, column)
					}
					rgb, set := glyph.UnderlineColor.RGB()
					if column < len("Red underline") && (!set || rgb != 0xff0000) || column >= len("Red underline") && set {
						t.Fatalf("split %d width %d character %d underline color = %06x, %v", split, width, column, rgb, set)
					}
					if _, set := glyph.Style.Foreground(); set {
						t.Fatal("underline color leaked into text foreground")
					}
					column++
				}
			}
		}
		text.Close()
		doc.Close()
	}
}

func TestUnderlineColorsReachTerminalCells(t *testing.T) {
	input, err := os.ReadFile("testdata/colored-underlines.txt")
	if err != nil {
		t.Fatal(err)
	}
	doc := pager.NewDocument()
	defer doc.Close()
	doc.Write(input)
	text := NewText(doc)
	defer text.Close()
	text.Update(80, true)
	lines, err := text.Lines(0, 4)
	if err != nil {
		t.Fatal(err)
	}
	screen, err := tcell.NewTerminfoScreenFromTty(vt.NewMockTerm(vt.MockOptSize{X: 80, Y: 4}), tcell.OptNegotiation(false))
	if err != nil {
		t.Fatal(err)
	}
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()
	model, _ := NewModel(0)
	if err := NewTerminal(screen).Render(Frame{Layout: model.Layout(80, 4), Lines: lines, Styled: text.Styled()}); err != nil {
		t.Fatal(err)
	}
	for x := range len(lines[0]) {
		_, style, _ := screen.Get(x, 0)
		if !style.HasUnderline() {
			t.Fatalf("terminal cell %d lost underline", x)
		}
		color := style.GetUnderlineColor()
		if x < len("Red underline") {
			r, g, b := color.RGB()
			if r != 255 || g != 0 || b != 0 {
				t.Fatalf("terminal cell %d underline = %v", x, color)
			}
		} else if color.Valid() {
			t.Fatalf("terminal cell %d did not reset underline color", x)
		}
	}
}

func TestUnderlineCheckpointsSurvivePagingAndEviction(t *testing.T) {
	for _, rolling := range []bool{false, true} {
		options := pager.DefaultStorageOptions()
		if rolling {
			options.MemoryBudget, options.Retention = 64, "rolling"
		}
		doc, _ := pager.NewDocumentWithOptions(16, options)
		text, _ := NewTextWithLimits(doc, 16<<10, 1<<20)
		doc.Write([]byte("\x1b[4;58;2;1;2;3m" + strings.Repeat("a", 32)))
		if err := text.Update(1, false); err != nil {
			t.Fatal(err)
		}
		count := 2000
		if rolling {
			count = 40
		}
		doc.Write([]byte(strings.Repeat("b", count)))
		for _, width := range []int{1, 3} {
			if err := text.Update(width, true); err != nil {
				t.Fatal(err)
			}
			top := max(0, text.RowCount()-10)
			if _, err := text.Lines(top, 10); err != nil {
				t.Fatal(err)
			}
			for _, row := range text.Styled() {
				for _, glyph := range row {
					rgb, set := glyph.UnderlineColor.RGB()
					if !set || rgb != 0x010203 {
						t.Fatalf("rolling %v lost checkpoint color %06x, %v", rolling, rgb, set)
					}
				}
			}
		}
		text.Close()
		doc.Close()
	}
}
