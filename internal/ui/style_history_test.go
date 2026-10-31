package ui

import (
	"strings"
	"testing"

	"github.com/araaha/glimpse/internal/pager"
)

func TestOnlyEvictingInputNeedsTransitionHistory(t *testing.T) {
	for _, retention := range []string{"lossless", "rolling"} {
		t.Run(retention, func(t *testing.T) {
			options := pager.DefaultStorageOptions()
			options.Retention = retention
			doc, err := pager.NewDocumentWithOptions(64, options)
			if err != nil {
				t.Fatal(err)
			}
			defer doc.Close()
			doc.Write([]byte(strings.Repeat("\x1b[31mRED\n\x1b[32mGREEN\n", 1000)))
			text := NewText(doc)
			defer text.Close()
			for _, width := range []int{2, 4, 80} {
				if err := text.Update(width, true); err != nil {
					t.Fatal(err)
				}
				if retention == "lossless" && text.styleOffsets.count != 1 {
					t.Fatal("lossless input retained per-transition metadata")
				}
				if retention == "rolling" && text.styleOffsets.count < 1000 {
					t.Fatal("rolling input lost eviction recovery metadata")
				}
				for line, want := range map[int]int{1: 1, 2: 2, 1999: 1, 2000: 2} {
					if _, err := text.Lines(text.LineRow(line), 1); err != nil {
						t.Fatal(err)
					}
					for _, glyph := range text.Styled()[0] {
						index, ok := glyph.Style.ForegroundColor().Index()
						if !ok || index != want {
							t.Fatalf("width %d line %d lost inherited color: %+v", width, line, glyph)
						}
					}
				}
			}
		})
	}
}

func TestSearchAnchorUsesDecodedLogicalLines(t *testing.T) {
	var input strings.Builder
	var starts []int64
	for range 80 {
		starts = append(starts, int64(input.Len()))
		input.WriteString("\x1b]52;hidden\ntext\a\x1b[31m👩‍💻visible line\x1b[m\n")
	}
	source := benchmarkSource{strings.NewReader(input.String())}
	text := NewText(source)
	defer text.Close()
	if err := text.Update(3, true); err != nil {
		t.Fatal(err)
	}
	for _, line := range []int{0, 20, 32, 50, 79} {
		if got, want := text.SearchAnchor(starts[line]+1), starts[max(0, line-32)]; got != want {
			t.Fatalf("line %d anchor = %d, want %d", line, got, want)
		}
	}
}
