package content

import (
	"io"
	"reflect"
	"strings"
	"testing"
)

func decodedClusters(t *testing.T, input string, batch, complete bool) []Cluster {
	t.Helper()
	reader := NewGraphemeReader(strings.NewReader(input), 0, int64(len(input)), 0, complete)
	var result []Cluster
	for {
		if batch {
			if run, ok := reader.ASCII(); ok {
				for i, b := range run.Bytes {
					result = append(result, Cluster{Token: Token{Rune: rune(b), Raw: rune(b), Start: run.Start + int64(i), End: run.Start + int64(i) + 1, Style: run.Style, UnderlineColor: run.UnderlineColor}, Text: string(run.Bytes[i : i+1]), AfterStyle: run.Style, AfterUnderlineColor: run.UnderlineColor})
				}
				continue
			}
		}
		cluster, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		result = append(result, cluster)
	}
	return result
}

func TestASCIIBatchingPreservesGraphemeAndStyleBoundaries(t *testing.T) {
	for _, input := range []string{
		strings.Repeat("a", 4095) + "é👩🏽‍💻🇺🇳❤️\n",
		strings.Repeat("a", 4095) + "\x1b[31ḿb\x1b[0m",
		"a\x1b[4;58:5:196mbc\x1b[59m d\t\r\b_\n\x1b[0m",
		"abc\xe7", "abc\x1b[31", "abc\x00def", "abc\n\x1b[31m",
	} {
		for _, complete := range []bool{false, true} {
			got, want := decodedClusters(t, input, true, complete), decodedClusters(t, input, false, complete)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("complete %v input %q: fast and general decoding differ", complete, input)
			}
		}
	}
}

func FuzzASCIIBatching(f *testing.F) {
	for _, input := range []string{"abc", "é👩🏽‍💻", "a\x1b[58:5:196mb\x1b[59mc", "\r\n\b\t\xff"} {
		f.Add(input)
	}
	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > 8192 {
			t.Skip()
		}
		got, want := decodedClusters(t, input, true, true), decodedClusters(t, input, false, true)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("batching changed decoding for %q", input)
		}
	})
}
