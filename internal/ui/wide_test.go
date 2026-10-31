package ui

import (
	"io"
	"strings"
	"testing"

	"github.com/araaha/glimpse/internal/content"

	"github.com/clipperhouse/displaywidth"
)

// TestWideRowsMatchClusterLayout checks the wide-rune fast path against row
// starts laid out cluster by cluster.
func TestWideRowsMatchClusterLayout(t *testing.T) {
	input := strings.Repeat("日本語のテキストとｶﾀｶﾅ、한국어 텍스트 mixed with ASCII.\n", 20) + "がき\x1b[1m漢字\x1b[m字́\n"
	for _, width := range []int{1, 2, 3, 7, 10, 41} {
		var want []int64
		column := 0
		reader := content.NewGraphemeReader(strings.NewReader(input), 0, int64(len(input)), 0, true)
		want = append(want, 0)
		for {
			cluster, err := reader.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			if cluster.Hidden {
				continue
			}
			if cluster.Rune == '\n' {
				want, column = append(want, cluster.End), 0
				continue
			}
			w := displaywidth.String(cluster.Text)
			if w > 0 && column > 0 && column+w > width {
				want, column = append(want, cluster.Start), 0
			}
			column += min(w, width)
		}
		text := NewText(benchmarkSource{strings.NewReader(input)})
		if err := text.Update(width, true); err != nil {
			t.Fatal(err)
		}
		if text.starts.count != len(want) {
			t.Fatalf("width %d: %d rows, want %d", width, text.starts.count, len(want))
		}
		for i, start := range want {
			if got := text.starts.at(i); got != start {
				t.Fatalf("width %d row %d starts at %d, want %d", width, i, got, start)
			}
		}
		text.Close()
	}
}
