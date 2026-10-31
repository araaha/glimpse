package app

import (
	"fmt"
	"strings"
	"testing"

	"github.com/araaha/glimpse/internal/ui"
)

func TestParseJumpTarget(t *testing.T) {
	for _, tt := range []struct {
		value   string
		rows    [3]int // rows for heights 1, 10, and 11
		display string
	}{
		{"1", [3]int{0, 0, 0}, "1"},
		{"1.0", [3]int{0, 0, 0}, "1"},
		{"0", [3]int{0, 9, 10}, "0"},
		{"0.0", [3]int{0, 9, 10}, "0"},
		{".0", [3]int{0, 9, 10}, "0"},
		{".5", [3]int{0, 5, 5}, ".5"},
		{"0.3", [3]int{0, 6, 7}, ".3"},
		{"0.25", [3]int{0, 7, 8}, ".25"},
		{"0.75", [3]int{0, 2, 3}, ".75"},
		{" 0.5 ", [3]int{0, 5, 5}, ".5"},
		{"5e-1", [3]int{0, 5, 5}, ".5"},
	} {
		target, err := ParseJumpTarget(tt.value)
		if err != nil {
			t.Fatalf("%s: %v", tt.value, err)
		}
		for i, height := range []int{1, 10, 11} {
			if got := target.Row(height); got != tt.rows[i] {
				t.Fatalf("%s at height %d = row %d, want %d", tt.value, height, got, tt.rows[i])
			}
		}
		if target.String() != tt.display {
			t.Fatalf("%s displays as %q", tt.value, target.String())
		}
		if got := target.Row(0); got != 0 {
			t.Fatalf("%s in an empty viewport = row %d", tt.value, got)
		}
		roundTrip, err := ParseJumpTarget(target.String())
		if err != nil || roundTrip != target {
			t.Fatalf("%s round trip = %+v, %v, want %+v", tt.value, roundTrip, err, target)
		}
	}
	for _, value := range []string{"", "2", "4", "-1", "1.5", "1.01", "-.5", "x", ".", "NaN", "nan", "Inf", "+Inf", "-Inf", "1e999"} {
		if _, err := ParseJumpTarget(value); err == nil {
			t.Fatalf("%q was accepted", value)
		}
	}
	if (JumpTarget{}).Row(10) != 0 || (JumpTarget{}).String() != "1" {
		t.Fatal("zero value must default to the top")
	}
}

func TestRunJumpTargetPositionsSearchesAndLineJumps(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	var input strings.Builder
	for line := 1; line <= 30; line++ {
		fmt.Fprintf(&input, "line%02d\n", line)
	}
	// cmdheight 1 keeps the body at five rows whether or not a message shows.
	for _, tt := range []struct {
		target string
		row    int
	}{{"1", 0}, {".75", 1}, {".5", 2}, {".25", 3}, {"0", 4}} {
		t.Run(tt.target, func(t *testing.T) {
			target, err := ParseJumpTarget(tt.target)
			if err != nil {
				t.Fatal(err)
			}
			viewer := startTestViewer(t, Options{CmdHeight: ui.CmdHeightWithSearch, JumpTarget: target}, input.String(), 8, 7)
			atTarget := func(line string) func(ui.Frame) bool {
				return func(frame ui.Frame) bool { return len(frame.Lines) > tt.row && frame.Lines[tt.row] == line }
			}
			viewer.press("/")
			viewer.typeText("line10")
			viewer.press("<Enter>")
			waitFrame(t, viewer.frames, atTarget("line10"))
			viewer.press("2", "0", "G")
			waitFrame(t, viewer.frames, atTarget("line20"))
			viewer.press(":")
			viewer.typeText("15")
			viewer.press("<Enter>")
			waitFrame(t, viewer.frames, atTarget("line15"))
			// Near the start, the view cannot scroll above the first line.
			viewer.press("1", "G")
			waitFrame(t, viewer.frames, firstFrameLine("line01"))
			// Without a selected match, n searches from the target row: after
			// scrolling down one row, line11 is on it and is found first.
			viewer.press("/")
			viewer.typeText("line1")
			viewer.press("<Enter>")
			waitFrame(t, viewer.frames, atTarget("line10"))
			viewer.press("j", "n")
			waitFrame(t, viewer.frames, atTarget("line11"))
			viewer.press("n")
			waitFrame(t, viewer.frames, atTarget("line12"))
			viewer.press("q")
			viewer.expectExit(t)
		})
	}
}
