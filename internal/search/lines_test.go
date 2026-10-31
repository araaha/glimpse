package search

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// referenceLines lists the matching lines of plain input as runs.
func referenceLines(input, pattern string, partial, keepFirst bool) LineScan {
	re := regexp.MustCompile(pattern)
	var scan LineScan
	offset := int64(0)
	lines := strings.SplitAfter(input, "\n")
	for i, line := range lines {
		complete := strings.HasSuffix(line, "\n")
		if !complete && (line == "" || !partial) {
			break
		}
		if re.MatchString(strings.TrimSuffix(line, "\n")) || keepFirst && i == 0 {
			end := offset + int64(len(line))
			if n := len(scan.Runs); n > 0 && scan.Runs[n-1].End == offset {
				scan.Runs[n-1].End, scan.Runs[n-1].Lines = end, scan.Runs[n-1].Lines+1
			} else {
				scan.Runs = append(scan.Runs, LineRun{Start: offset, End: end, Line: int64(i), Lines: 1})
			}
			scan.TailMatched = !complete
		}
		if complete {
			scan.Lines++
			offset += int64(len(line))
		}
	}
	scan.Tail = offset
	return scan
}

func TestMatchingLines(t *testing.T) {
	var input strings.Builder
	for i := range 400 {
		fmt.Fprintf(&input, "line %d %s\n", i, strings.Repeat("x", i%37))
	}
	input.WriteString("tail line 7")
	for _, pattern := range []string{"7", "^line 1", "x{30}$", "nothing", "tail"} {
		for _, size := range []int64{1, 64, 1000, 1 << 20} {
			for _, partial := range []bool{false, true} {
				for _, keepFirst := range []bool{false, true} {
					name := fmt.Sprintf("%q/%d/partial=%v/first=%v", pattern, size, partial, keepFirst)
					t.Run(name, func(t *testing.T) {
						source := benchmarkSource{strings.NewReader(input.String())}
						query := Query{Text: pattern}
						got, err := MatchingLines(context.Background(), source, query, 0, source.Len(), partial, keepFirst, Options{ChunkSize: size, Workers: 3})
						if err != nil {
							t.Fatal(err)
						}
						want := referenceLines(input.String(), pattern, partial, keepFirst)
						if fmt.Sprint(got) != fmt.Sprint(want) {
							t.Fatalf("got  %+v\nwant %+v", got, want)
						}
					})
				}
			}
		}
	}
}

// A newline inside an OSC string is hidden, so it neither ends a line nor
// lets a chunk start inside the string.
func TestMatchingLinesHiddenNewline(t *testing.T) {
	input := "one\n\x1b]8;;a\nb\x1b\\two\nthree\n"
	source := benchmarkSource{strings.NewReader(input)}
	for _, size := range []int64{1, 4, 64} {
		got, err := MatchingLines(context.Background(), source, Query{Text: "two"}, 0, source.Len(), false, false, Options{ChunkSize: size, Workers: 2})
		if err != nil {
			t.Fatal(err)
		}
		start := int64(strings.Index(input, "\x1b]"))
		end := int64(strings.Index(input, "three"))
		want := LineScan{Runs: []LineRun{{Start: start, End: end, Line: 1, Lines: 1}}, Lines: 3, Tail: int64(len(input))}
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("size %d: got %+v, want %+v", size, got, want)
		}
	}
}
