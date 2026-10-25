package content

import (
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
)

func TestGraphemeClustersAndStyleInsideClusters(t *testing.T) {
	input := "a👩🏽‍💻🇺🇳e\x1b[31m\u0301❤️1️⃣"
	reader := NewGraphemeReader(strings.NewReader(input), 0, int64(len(input)), 0, true)
	var got []string
	for {
		cluster, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if !cluster.Hidden {
			got = append(got, cluster.Text)
		}
	}
	want := []string{"a", "👩🏽‍💻", "🇺🇳", "é", "❤️", "1️⃣"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("clusters = %q, want %q", got, want)
	}
}

func TestGraphemeSafetyLimitDoesNotBecomeWrapBoundary(t *testing.T) {
	for _, extra := range []int{0, 2} {
		input := "é" + strings.Repeat("\u0301", (MaxClusterBytes-2+extra)/2) + "b"
		reader := NewGraphemeReader(strings.NewReader(input), 0, int64(len(input)), 0, true)
		cluster, err := reader.Next()
		if extra == 0 && (err != nil || len(cluster.Text) != MaxClusterBytes) {
			t.Fatalf("exact limit: %d, %v", len(cluster.Text), err)
		}
		if extra > 0 && (err == nil || !strings.Contains(err.Error(), "safety limit")) {
			t.Fatalf("oversized cluster silently split: %v", err)
		}
	}
}

func TestTrailingNewlineDoesNotAbsorbFollowingStyle(t *testing.T) {
	input := "a\n\x1b[31m"
	reader := NewGraphemeReader(strings.NewReader(input), 0, int64(len(input)), 0, false)
	reader.Next()
	newline, err := reader.Next()
	if err != nil || newline.Text != "\n" || newline.End != 2 || newline.AfterStyle != 0 {
		t.Fatalf("newline swallowed style: %+v, %v", newline, err)
	}
	style, err := reader.Next()
	if err != nil || !style.Hidden || style.Start != 2 || style.End != int64(len(input)) {
		t.Fatalf("trailing style token: %+v, %v", style, err)
	}
}

type readCounter struct {
	*strings.Reader
	bytes int
}

func (r *readCounter) ReadAt(p []byte, offset int64) (int, error) {
	n, err := r.Reader.ReadAt(p, offset)
	r.bytes += n
	return n, err
}

func TestRenderPrefetchStopsAtBoundedHiddenSuffix(t *testing.T) {
	input := "x" + strings.Repeat("\x1b[0m", 20000)
	source := &readCounter{Reader: strings.NewReader(input)}
	reader := NewGraphemeReader(source, 0, int64(len(input)), 0, true)
	reader.SetRenderState(1, 0, 0, false)
	cluster, err := reader.Next()
	if err != nil || cluster.Text != "x" || source.bytes > 4096 {
		t.Fatalf("prefetch = %q, %d bytes, %v", cluster.Text, source.bytes, err)
	}
}

func FuzzBoundedGraphemeReader(f *testing.F) {
	for _, input := range []string{"é👩🏽‍💻🇺🇳", "a\x1b[31ḿb", "\r\n\b\t", "\xff\x1b]52;secret\a"} {
		f.Add(input)
	}
	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > 16384 {
			t.Skip()
		}
		reader := NewGraphemeReader(strings.NewReader(input), 0, int64(len(input)), 0, true)
		last := int64(0)
		for steps := 0; ; steps++ {
			cluster, err := reader.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			if cluster.End <= cluster.Start || cluster.Start < last || cluster.End > int64(len(input)) || steps > len(input)+1 {
				t.Fatalf("invalid cluster range: %+v", cluster)
			}
			last = cluster.End
		}
	})
}
