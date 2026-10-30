package content

import (
	"io"
	"strings"
	"testing"

	"github.com/clipperhouse/displaywidth"
	"github.com/clipperhouse/uax29/v2/graphemes"
)

// TestWideRunesMatchUnicode checks WideRune's promise against the segmenter
// and width tables: two cells, and a boundary before ASCII or another one.
func TestWideRunesMatchUnicode(t *testing.T) {
	followers := []rune{0x4e00, 0x9fff, 0x3400, 0x3042, 0x30a2, 0xac00, 0xac01, 0xd7a3, 0xff01, 0xffe6}
	for r := range rune(128) {
		followers = append(followers, r)
	}
	for r := rune(0); r <= 0xffff; r++ {
		if !WideRune(r) {
			continue
		}
		for _, wide := range []bool{false, true} {
			if width := (displaywidth.Options{EastAsianWidth: wide}).String(string(r)); width != 2 {
				t.Fatalf("U+%04X is %d cells wide", r, width)
			}
		}
		for _, next := range followers {
			text := string(r) + string(next)
			iterator := graphemes.FromString(text)
			if !iterator.Next() || iterator.Value() != string(r) {
				t.Fatalf("U+%04X U+%04X: first cluster %q", r, next, iterator.Value())
			}
		}
	}
}

// TestWideRunsMatchNext checks that batching wide runes yields the clusters
// Next would, including styles, hidden escapes, and combining marks.
func TestWideRunsMatchNext(t *testing.T) {
	for _, input := range []string{
		"日本語のテキスト\n한국어 텍스트\n",
		"漢字\x1b[31m赤い\x1b[m字\n",
		"がき́く\nｱｲｳＡＢＣ\n",
		"字\u200d字\U0001F600字\n",
		strings.Repeat("中文", 300) + "\n",
	} {
		var want, got []Cluster
		reader := NewGraphemeReader(strings.NewReader(input), 0, int64(len(input)), 0, true)
		for {
			cluster, err := reader.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			want = append(want, cluster)
		}
		reader = NewGraphemeReader(strings.NewReader(input), 0, int64(len(input)), 0, true)
		for {
			if run, ok := reader.Wide(); ok {
				for _, token := range run {
					got = append(got, Cluster{Token: token, Text: string(token.Rune), AfterStyle: token.Style, AfterUnderlineColor: token.UnderlineColor})
				}
				continue
			}
			cluster, err := reader.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			got = append(got, cluster)
		}
		if len(got) != len(want) {
			t.Fatalf("%q: %d clusters, want %d", input, len(got), len(want))
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%q cluster %d = %+v, want %+v", input, i, got[i], want[i])
			}
		}
	}
}
