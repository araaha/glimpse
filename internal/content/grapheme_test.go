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
