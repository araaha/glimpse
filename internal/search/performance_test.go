package search

import (
	"bytes"
	"context"
	"glimpse/internal/content"
	"os"
	"strings"
	"testing"
)

type benchmarkSource struct{ *strings.Reader }

func (s benchmarkSource) Len() int64 { return s.Size() }

func BenchmarkFind(b *testing.B) {
	data := strings.Repeat("\x1b[33mcommit\x1b[m\n\x1b[32m+ordinary patch content\x1b[m\n", 250000)
	if path := os.Getenv("GLIMPSE_BENCH_FILE"); path != "" {
		input, err := os.ReadFile(path)
		if err != nil {
			b.Fatal(err)
		}
		data = string(input)
	}
	source := benchmarkSource{strings.NewReader(data)}
	anchor := int64(0)
	reader := content.NewPlainReader(source, 0, source.Len())
	for {
		if run, ok := reader.ASCII(); ok {
			if i := bytes.LastIndexByte(run.Bytes, '\n'); i >= 0 && run.Start+int64(i)+1 < source.Len()-10000 {
				anchor = run.Start + int64(i) + 1
			}
			continue
		}
		token, err := reader.Next()
		if err != nil {
			break
		}
		if token.Raw == '\n' && token.End < source.Len()-10000 {
			anchor = token.End
		}
	}
	for _, tt := range []struct {
		name, query string
		origin      int64
		backward    bool
		anchored    bool
	}{
		{"absent", "ABSENT-SEARCH-MARKER", 0, false, false},
		{"next-near-end", "commit", source.Len() - 10000, false, false},
		{"previous-near-end", "commit", source.Len() - 1, true, false},
		{"anchored-next-near-end", "commit", source.Len() - 10000, false, true},
		{"anchored-previous-near-end", "commit", source.Len() - 1, true, true},
	} {
		b.Run(tt.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				start := int64(0)
				if tt.anchored {
					start = anchor
				}
				if _, err := FindFrom(context.Background(), source, tt.query, tt.origin, start, tt.backward, false, false, false); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
