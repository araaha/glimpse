package ui

import (
	"reflect"
	"testing"

	"glimpse/internal/pager"

	"github.com/gdamore/tcell/v3"
	"github.com/gdamore/tcell/v3/color"
	"github.com/gdamore/tcell/v3/vt"
)

func TestScanHexColorsNeedsStandaloneSixDigits(t *testing.T) {
	type code struct {
		start, end int64
		rgb        int32
	}
	for _, tt := range []struct {
		input string
		want  []code
	}{
		{"color: #ff8800;", []code{{7, 14, 0xff8800}}},
		{"#AbCdEf", []code{{0, 7, 0xabcdef}}},
		{"(#000000,#FFFFFF)", []code{{1, 8, 0}, {9, 16, 0xffffff}}},
		{"#fff #ff88 #ff880011 #ff880g", nil},
		{"issue #123456789 a#ff8800 ##ff8800 #ff8800_", nil},
		{"\x1b[31m#ff\x1b[0m8800\x1b[0m.", []code{{5, 16, 0xff8800}}},
		{"#ff8800é", nil},
	} {
		doc := pager.NewDocument()
		doc.Write([]byte(tt.input))
		var got []code
		scanHexColors(doc, 0, doc.Len(), func(start, end int64, rgb int32) { got = append(got, code{start, end, rgb}) })
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%q: got %+v, want %+v", tt.input, got, tt.want)
		}
	}
}

func TestScanHexColorsWaitsForTheFollowingRune(t *testing.T) {
	doc := pager.NewDocument()
	doc.Write([]byte("#ff8800 #00ff00"))
	count := 0
	scanHexColors(doc, 0, 11, func(int64, int64, int32) { count++ })
	if count != 1 {
		t.Fatalf("a code cut by the range end counted: %d", count)
	}
}

func TestHexColorsCoverWrappedAndScrolledCodes(t *testing.T) {
	doc := pager.NewDocument()
	doc.Write([]byte("ab #123456\nx #abcdef"))
	text := NewText(doc)
	if err := text.Update(6, true); err != nil {
		t.Fatal(err)
	}
	lines, _ := text.Lines(0, 10)
	got := text.HexColors(lines, 0)
	want := []HexColor{{0, 3, 1, 0x123456}, {0, 4, 1, 0x123456}, {0, 5, 1, 0x123456}}
	if len(got) != 14 || !reflect.DeepEqual(got[:3], want) || got[3] != (HexColor{1, 0, 1, 0x123456}) || got[13] != (HexColor{3, 2, 1, 0xabcdef}) {
		t.Fatalf("wrapped code = %+v", got)
	}

	unwrapped, _ := unwrappedText(t, "ab #123456\nx #abcdef", 6)
	unwrapped.SetLeft(4)
	lines, _ = unwrapped.Lines(0, 2)
	got = unwrapped.HexColors(lines, 0)
	if len(got) != 6+5 || got[0] != (HexColor{0, 0, 1, 0x123456}) || got[6] != (HexColor{1, 0, 1, 0xabcdef}) {
		t.Fatalf("scrolled codes = %+v", got)
	}
}

func TestRenderHexColorsBeneathHighlights(t *testing.T) {
	screen, err := tcell.NewTerminfoScreenFromTty(vt.NewMockTerm(vt.MockOptSize{X: 10, Y: 2}), tcell.OptNegotiation(false))
	if err != nil {
		t.Fatal(err)
	}
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()
	frame := Frame{
		Layout: Layout{Body: Viewport{Width: 10, Height: 2}, StatusRow: -1, SearchRow: -1},
		Lines:  []string{"#ffff00 x"}, Styled: [][]Glyph{{{Text: "#", Width: 1}, {Text: "f", Column: 1, Width: 1}, {Text: "0", Column: 2, Width: 1}}},
		HexColors:  []HexColor{{0, 0, 1, 0xffff00}, {0, 1, 1, 0xffff00}, {0, 2, 1, 0x000080}},
		Highlights: []Highlight{{Row: 0, Column: 1, Width: 1}},
	}
	if err := NewTerminal(screen).Render(frame); err != nil {
		t.Fatal(err)
	}
	if _, style, _ := screen.Get(0, 0); style.GetBackground() != color.NewHexColor(0xffff00) || style.GetForeground() != color.NewHexColor(0) {
		t.Fatalf("light code style = %+v", style)
	}
	if _, style, _ := screen.Get(2, 0); style.GetForeground() != color.NewHexColor(0xffffff) {
		t.Fatalf("dark code text = %+v", style.GetForeground())
	}
	if _, style, _ := screen.Get(1, 0); !style.HasReverse() {
		t.Fatal("search highlight not drawn over the hex color")
	}
}
