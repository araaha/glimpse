package app

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/araaha/glimpse/internal/search"
	"github.com/araaha/glimpse/internal/ui"
)

func TestRunStatuslineModesCountsAndLogicalLines(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	viewer := startTestViewer(t, Options{}, "header\nhit one\ngap\nhit two\nhit three\nend", 60, 4)
	viewer.press("=")
	frame := waitFrame(t, viewer.frames, func(frame ui.Frame) bool { return frame.StatusState.TotalLinesKnown })
	if frame.StatusState.Mode != "VIEW" || frame.StatusState.Filename != ui.FormatFilename("test", "") || frame.StatusState.TopLine != 1 || frame.StatusState.TotalLines != 6 {
		t.Fatalf("initial status = %+v", frame.StatusState)
	}
	viewer.press("/")
	frame = waitFrame(t, viewer.frames, searchFrame("/", true))
	if frame.StatusState.Mode != "SEARCH" || frame.Layout.StatusRow < 0 {
		t.Fatal("search mode/status disappeared while editing")
	}
	viewer.typeText("hit")
	viewer.press("<Enter>")
	checkResult := func(current int64, top int) {
		t.Helper()
		frame = waitFrame(t, viewer.frames, func(frame ui.Frame) bool {
			s := frame.StatusState
			return s.Mode == "VIEW" && s.SearchCurrentKnown && s.SearchTotalKnown && s.SearchCurrent == current
		})
		if frame.StatusState.SearchTotal != 3 || frame.StatusState.TopLine != top || !strings.Contains(frame.Status, "/3") || !strings.Contains(frame.Status, "/6") {
			t.Fatalf("search status = %+v, %q", frame.StatusState, frame.Status)
		}
	}
	checkResult(1, 2)
	viewer.press("n")
	checkResult(2, 4)
	viewer.press("N")
	checkResult(1, 2)
	viewer.press("N")
	checkResult(3, 5)
	viewer.press("?")
	frame = waitFrame(t, viewer.frames, searchFrame("?", true))
	if frame.StatusState.Mode != "SEARCH" {
		t.Fatal("backward prompt did not enter SEARCH mode")
	}
	viewer.press("<Esc>")
	checkResult(3, 4) // Closing the prompt expands the viewport and fills EOF.
	viewer.press("F")
	frame = waitFrame(t, viewer.frames, func(frame ui.Frame) bool { return frame.StatusState.Mode == "FOLLOW" })
	if !frame.Follow {
		t.Fatal("FOLLOW label disagrees with actual follow state")
	}
	viewer.press(":")
	frame = waitFrame(t, viewer.frames, searchFrame(":", true))
	if frame.StatusState.Mode != "FOLLOW" || !frame.Follow {
		t.Fatalf("command prompt left follow mode: %s", frame.StatusState.Mode)
	}
	viewer.press("<Esc>")
	waitFrame(t, viewer.frames, func(frame ui.Frame) bool { return !frame.SearchActive && frame.Follow })
	viewer.press("q")
	viewer.expectExit(t)
}

func TestRunStatuslineExternalSample(t *testing.T) {
	path := os.Getenv("GLIMPSE_BENCH_FILE")
	if path == "" {
		t.Skip("set GLIMPSE_BENCH_FILE to validate asynchronous counts on an external sample")
	}
	input, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	source := countTestSource{strings.NewReader(string(input))}
	var total int64
	if err := search.Walk(context.Background(), source, search.Query{Text: "commit"}, 0, source.Len(), func(start, end int64) bool { total++; return true }); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	viewer := startTestViewer(t, Options{}, string(input), 100, 24)
	viewer.press("/", "commit", "<Enter>")
	frame := waitFrame(t, viewer.frames, func(frame ui.Frame) bool {
		s := frame.StatusState
		return s.Mode == "VIEW" && s.SearchTotalKnown && s.SearchCurrentKnown && s.TotalLinesKnown
	})
	if frame.StatusState.SearchTotal != total {
		t.Fatalf("worker total = %d, want %d", frame.StatusState.SearchTotal, total)
	}
	if total > 0 && frame.StatusState.SearchCurrent != 1 {
		t.Fatalf("first match rank = %d", frame.StatusState.SearchCurrent)
	}
	t.Logf("status settled: match %d/%d, logical line %d/%d", frame.StatusState.SearchCurrent, total, frame.StatusState.TopLine, frame.StatusState.TotalLines)
	viewer.press("q")
	viewer.expectExit(t)
}

func TestRunStatuslineUsesLogicalNotWrappedLines(t *testing.T) {
	viewer := startTestViewer(t, Options{}, "abcdefghijkl\nsecond\nthird", 4, 4)
	viewer.press("j")
	frame := waitFrame(t, viewer.frames, firstFrameLine("efgh"))
	if frame.StatusState.TopLine != 1 || frame.StatusState.TotalLines != 3 || !frame.StatusState.TotalLinesKnown {
		t.Fatalf("wrapped row counted as a logical line: %+v", frame.StatusState)
	}
	viewer.press("2", "G")
	frame = waitFrame(t, viewer.frames, firstFrameLine("seco"))
	if frame.StatusState.TopLine != 2 {
		t.Fatalf("logical jump status = %+v", frame.StatusState)
	}
	viewer.press("q")
	viewer.expectExit(t)
}

func TestRunStatuslineSelectionAndHighlightConfiguration(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	options := Options{Statusline: ui.StatuslineConfig{Left: []string{"MODE"}, Right: []string{"LINE_NUMBERS"}}, SearchHighlight: ui.Colors{FG: "#010203", BG: "#040506"}}
	viewer := startTestViewer(t, options, "head\nhit\nend", 40, 4)
	viewer.press("/", "hit", "<Enter>")
	frame := waitFrame(t, viewer.frames, searchFrame("/hit", false))
	for _, span := range frame.StatusBar.Spans {
		if span.Section == ui.StatusFilename || span.Section == ui.StatusSearchResults {
			t.Fatalf("disabled section appeared: %+v", span)
		}
	}
	fg, bg := frame.HighlightStyle.GetForeground(), frame.HighlightStyle.GetBackground()
	if !fg.Valid() || !bg.Valid() || !fg.IsRGB() || !bg.IsRGB() {
		t.Fatal("highlight colors did not reach frame")
	}
	if frame.StatusState.Mode != "VIEW" || len(frame.Highlights) == 0 {
		t.Fatal("custom colors affected search/mode behavior")
	}
	viewer.press("q")
	viewer.expectExit(t)
}

func TestRunStatuslineOmitsStdinAndHandlesEmptyInput(t *testing.T) {
	for _, input := range []string{"one\ntwo", ""} {
		viewer := startNamedTestViewer(t, Options{}, input, "", 50, 4)
		viewer.press("=")
		frame := waitFrame(t, viewer.frames, func(frame ui.Frame) bool { return frame.StatusState.TotalLinesKnown })
		if frame.StatusState.Filename != "" || strings.Contains(frame.Status, "stdin") {
			t.Fatalf("stdin filename displayed: %q", frame.Status)
		}
		if frame.StatusBar.Has(ui.StatusFilename) {
			t.Fatal("empty filename has a section")
		}
		if input == "" && (frame.StatusState.TopLine != 0 || frame.StatusState.TotalLines != 0) {
			t.Fatalf("empty input lines = %+v", frame.StatusState)
		}
		viewer.press("q")
		viewer.expectExit(t)
	}
}
