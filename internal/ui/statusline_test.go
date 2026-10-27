package ui

import (
	"strings"
	"testing"
	"unicode/utf8"

	"glimpse/internal/content"

	"github.com/gdamore/tcell/v3"
	"github.com/gdamore/tcell/v3/color"
	"github.com/gdamore/tcell/v3/vt"
)

func testStatusState() StatusState {
	return StatusState{Mode: "VIEW", Filename: "file.txt", HasSearch: true, SearchCurrent: 3, SearchTotal: 27, SearchCurrentKnown: true, SearchTotalKnown: true, TopLine: 842, TotalLines: 1200, TotalLinesKnown: true}
}

func TestStatuslineDefaultLayoutAndColors(t *testing.T) {
	model, err := NewStatusline(StatuslineConfig{})
	if err != nil {
		t.Fatal(err)
	}
	bar := model.Format(testStatusState(), 80, false)
	if !strings.HasPrefix(bar.Text(), "VIEW │ file.txt") || !strings.HasSuffix(bar.Text(), "3/27 │ 842/1200") {
		t.Fatalf("status = %q", bar.Text())
	}
	want := map[string]struct {
		text   string
		fg, bg int32
	}{
		StatusMode: {"VIEW", 0, 0x0000ff}, StatusFilename: {"file.txt", 0x0000ff, 0}, StatusSearchResults: {"3/27", 0, 0xffffff}, StatusLineNumbers: {"842/1200", 0, 0x0000ff},
	}
	for _, span := range bar.Spans {
		if span.Section == "" {
			continue
		}
		value := want[span.Section]
		if span.Text != value.text || span.Style.GetForeground() != color.NewHexColor(value.fg) || span.Style.GetBackground() != color.NewHexColor(value.bg) || span.Style.HasReverse() {
			t.Fatalf("section %s = %+v", span.Section, span)
		}
		delete(want, span.Section)
	}
	if len(want) != 0 {
		t.Fatalf("missing sections: %v", want)
	}
	if bar.Style.GetBackground() != color.NewHexColor(0) {
		t.Fatal("bar background is not black")
	}
}

func TestStatuslineOmissionsOrderingAndInheritance(t *testing.T) {
	separator := " :: "
	model, err := NewStatusline(StatuslineConfig{Left: []string{"FILENAME", "MODE"}, Right: []string{"LINE_NUMBERS"}, Separator: &separator, BG: "#112233", Styles: map[string]Colors{StatusMode: {BG: "#445566"}, StatusFilename: {FG: "#778899"}}})
	if err != nil {
		t.Fatal(err)
	}
	state := testStatusState()
	state.Filename, state.HasSearch = "", false
	bar := model.Format(state, 40, false)
	if !strings.HasPrefix(bar.Text(), "VIEW") || strings.Contains(bar.Text(), "::") || strings.Contains(bar.Text(), "3/27") {
		t.Fatalf("omitted filename/search left separators: %q", bar.Text())
	}
	state.Filename = "example"
	bar = model.Format(state, 40, false)
	if !strings.HasPrefix(bar.Text(), "example :: VIEW") {
		t.Fatalf("configured order/separator = %q", bar.Text())
	}
	for _, span := range bar.Spans {
		if span.Section == StatusFilename && (span.Style.GetBackground() != color.NewHexColor(0x112233) || span.Style.GetForeground() != color.NewHexColor(0x778899)) {
			t.Fatal("filename did not inherit bar bg/override fg")
		}
		if span.Section == StatusMode && (span.Style.GetBackground() != color.NewHexColor(0x445566) || span.Style.GetForeground() != color.NewHexColor(0)) {
			t.Fatal("partial mode style lost default foreground")
		}
	}
	empty, _ := NewStatusline(StatuslineConfig{Left: []string{}, Right: []string{}})
	if bar := empty.Format(state, 20, false); len(bar.Spans) != 0 || bar.Text() != strings.Repeat(" ", 20) {
		t.Fatalf("empty configuration = %+v", bar)
	}
	separator = ""
	joined, _ := NewStatusline(StatuslineConfig{Separator: &separator, Right: []string{}})
	if got := joined.Format(state, 40, false).Text(); !strings.HasPrefix(got, "VIEWexample") {
		t.Fatalf("empty separator = %q", got)
	}
}

func TestStatuslinePendingCountsAndNarrowUnicode(t *testing.T) {
	model, _ := NewStatusline(StatuslineConfig{})
	state := testStatusState()
	state.SearchCurrentKnown, state.SearchTotalKnown, state.TotalLinesKnown = false, false, false
	state.Filename = "👩🏽‍💻-very-long-界-é-\x1b[31mfilename"
	if got := model.Format(state, 100, false).Text(); !strings.Contains(got, "…/…") || !strings.Contains(got, "842/…") {
		t.Fatalf("pending counts = %q", got)
	}
	for _, wide := range []bool{false, true} {
		for width := 0; width <= 100; width++ {
			bar := model.Format(state, width, wide)
			end := 0
			for _, span := range bar.Spans {
				if span.Column < end || span.Column+span.Width > width || !utf8.ValidString(span.Text) || strings.ContainsAny(span.Text, "\x1b\n\r\t") {
					t.Fatalf("width %d invalid/overlapping span: %+v", width, span)
				}
				if statusWidth(span.Text, wide) != span.Width {
					t.Fatalf("incorrect span width: %+v", span)
				}
				end = span.Column + span.Width
			}
			if got := statusWidth(bar.Text(), wide); got > width {
				t.Fatalf("bar width %d > %d: %q", got, width, bar.Text())
			}
		}
	}
}

func TestNarrowStatuslineKeepsModeAndLinePosition(t *testing.T) {
	model, _ := NewStatusline(StatuslineConfig{})
	bar := model.Format(testStatusState(), 15, false)
	seen := make(map[string]string)
	for _, span := range bar.Spans {
		if span.Section != "" {
			seen[span.Section] = span.Text
		}
	}
	if seen[StatusMode] != "VIEW" || seen[StatusLineNumbers] != "842/1200" || seen[StatusSearchResults] != "" || seen[StatusFilename] != "" {
		t.Fatalf("narrow fields = %v", seen)
	}
}

func TestStatuslineRejectsInvalidConfiguration(t *testing.T) {
	bad := "\x1b[31m"
	for _, config := range []StatuslineConfig{
		{Left: []string{"unknown"}}, {Left: []string{"MODE"}, Right: []string{"mode"}}, {FG: "red"}, {BG: "#xyz123"}, {Separator: &bad},
		{Styles: map[string]Colors{"unknown": {FG: "#ffffff"}}}, {Styles: map[string]Colors{StatusMode: {BG: "#123"}}},
	} {
		if _, err := NewStatusline(config); err == nil {
			t.Fatalf("accepted invalid statusline: %+v", config)
		}
	}
}

func TestStatuslineAndHighlightStylesReachTerminal(t *testing.T) {
	screen, err := tcell.NewTerminfoScreenFromTty(vt.NewMockTerm(vt.MockOptSize{X: 80, Y: 3}), tcell.OptNegotiation(false))
	if err != nil {
		t.Fatal(err)
	}
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()
	model, _ := NewStatusline(StatuslineConfig{})
	bar := model.Format(testStatusState(), 80, false)
	reader := content.NewReader(strings.NewReader("\x1b[1;4;7;31;42m界"), 0, int64(len("\x1b[1;4;7;31;42m界")), 0, true)
	token, _ := reader.Next()
	for token.Hidden {
		token, _ = reader.Next()
	}
	for _, colors := range []Colors{{}, {FG: "#010203", BG: "#040506"}, {BG: "#040506"}, {FG: "#010203"}} {
		highlight, _ := colors.Style(tcell.StyleDefault)
		frame := Frame{Layout: Layout{Body: Viewport{Width: 80, Height: 2}, StatusRow: 2, SearchRow: -1}, StatusBar: bar, Styled: [][]Glyph{{{Text: "界", Column: 0, Width: 2, Style: token.Style}}}, Highlights: []Highlight{{Row: 0, Column: 0, Width: 2}}, HighlightStyle: highlight}
		if err := NewTerminal(screen).Render(frame); err != nil {
			t.Fatal(err)
		}
		_, got, width := screen.Get(0, 0)
		if width != 2 || !got.HasBold() || !got.HasUnderline() {
			t.Fatalf("highlight damaged wide glyph/attributes: %+v", got)
		}
		if colors == (Colors{}) {
			if !got.HasReverse() {
				t.Fatal("default reverse highlight changed")
			}
		} else {
			if got.HasReverse() {
				t.Fatal("custom highlight still reverses its configured colors")
			}
			want := terminalStyle(token.Style).Reverse(false)
			want, _ = colors.Style(want)
			if got.GetForeground() != want.GetForeground() || got.GetBackground() != want.GetBackground() {
				t.Fatalf("highlight colors = %v/%v, want %v/%v", got.GetForeground(), got.GetBackground(), want.GetForeground(), want.GetBackground())
			}
		}
		for _, span := range bar.Spans {
			_, got, _ := screen.Get(span.Column, 2)
			if got.GetForeground() != span.Style.GetForeground() || got.GetBackground() != span.Style.GetBackground() {
				t.Fatalf("section %s lost terminal style", span.Section)
			}
		}
		_, filler, _ := screen.Get(30, 2)
		if filler.GetBackground() != color.NewHexColor(0) {
			t.Fatal("status filler is not black")
		}
	}
}

func TestStatuslineTerminalRespectsAmbiguousWidth(t *testing.T) {
	screen, err := tcell.NewTerminfoScreenFromTty(vt.NewMockTerm(vt.MockOptSize{X: 8, Y: 2}), tcell.OptNegotiation(false))
	if err != nil {
		t.Fatal(err)
	}
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()
	model, _ := NewStatusline(StatuslineConfig{Left: []string{StatusFilename}, Right: []string{}, Styles: map[string]Colors{StatusFilename: {BG: "#112233"}}})
	bar := model.Format(StatusState{Filename: "ΩX"}, 8, true)
	frame := Frame{Layout: Layout{Body: Viewport{Width: 8, Height: 1}, StatusRow: 1, SearchRow: -1}, StatusBar: bar, WideAmbiguous: true}
	if err := NewTerminal(screen).Render(frame); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := screen.Get(2, 1); got != "X" {
		t.Fatalf("ambiguous width shifted next character: %q", got)
	}
	if _, style, _ := screen.Get(1, 1); style.GetBackground() != color.NewHexColor(0x112233) {
		t.Fatal("wide ambiguous padding lost section background")
	}
}

func TestStatuslineAndHighlightEmitConfiguredRGB(t *testing.T) {
	t.Setenv("COLORTERM", "truecolor")
	t.Setenv("NO_COLOR", "")
	t.Setenv("TCELL_TRUECOLOR", "")
	tty := &colorOutputTerm{MockTerm: vt.NewMockTerm(vt.MockOptSize{X: 80, Y: 3}, vt.MockOptNoBlit{})}
	screen, err := tcell.NewTerminfoScreenFromTty(tty, tcell.OptTerm("xterm-256color"), tcell.OptNegotiation(false))
	if err != nil {
		t.Fatal(err)
	}
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()
	model, _ := NewStatusline(StatuslineConfig{})
	highlight, _ := (Colors{FG: "#010203", BG: "#040506"}).Style(tcell.StyleDefault)
	frame := Frame{Layout: Layout{Body: Viewport{Width: 80, Height: 2}, StatusRow: 2, SearchRow: -1}, Lines: []string{"hit"}, Highlights: []Highlight{{Row: 0, Column: 0, Width: 3}}, HighlightStyle: highlight, StatusBar: model.Format(testStatusState(), 80, false)}
	if err := NewTerminal(screen).Render(frame); err != nil {
		t.Fatal(err)
	}
	if err := tty.Drain(); err != nil {
		t.Fatal(err)
	}
	for _, sequence := range []string{"\x1b[38;2;1;2;3;48;2;4;5;6m", "\x1b[38;2;0;0;0;48;2;0;0;255m", "\x1b[38;2;0;0;0;48;2;255;255;255m"} {
		if !strings.Contains(tty.output.String(), sequence) {
			t.Fatalf("missing configured RGB output %q in %q", sequence, tty.output.String())
		}
	}
}

func FuzzStatuslineFitsWidth(f *testing.F) {
	f.Add("👩🏽‍💻-é-界", uint8(40), false)
	f.Add("\x1b]52;unsafe\a\n", uint8(2), true)
	model, _ := NewStatusline(StatuslineConfig{})
	f.Fuzz(func(t *testing.T, filename string, width uint8, wide bool) {
		if len(filename) > 1024 {
			t.Skip()
		}
		state := testStatusState()
		state.Filename = filename
		bar := model.Format(state, int(width), wide)
		end := 0
		for _, span := range bar.Spans {
			if span.Column < end || span.Column+span.Width > int(width) || !utf8.ValidString(span.Text) {
				t.Fatalf("invalid span: %+v", span)
			}
			end = span.Column + span.Width
		}
		if statusWidth(bar.Text(), wide) > int(width) {
			t.Fatalf("status overflows width %d: %q", width, bar.Text())
		}
	})
}
