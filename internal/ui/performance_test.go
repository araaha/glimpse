package ui

import (
	"os"
	"strings"
	"testing"

	"github.com/araaha/glimpse/internal/pager"
)

func BenchmarkSampleIndex(b *testing.B) {
	path := os.Getenv("GLIMPSE_BENCH_FILE")
	if path == "" {
		b.Skip("set GLIMPSE_BENCH_FILE to benchmark an external sample")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		b.Fatal(err)
	}
	source := benchmarkSource{strings.NewReader(string(data))}
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	for b.Loop() {
		text := NewText(source)
		if err := text.Update(100, true); err != nil {
			b.Fatal(err)
		}
		if err := text.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

type benchmarkSource struct{ *strings.Reader }

func (s benchmarkSource) Len() int64 { return s.Size() }

func BenchmarkTextIndex(b *testing.B) {
	for name, unit := range map[string]string{
		"ascii":   strings.Repeat("a", 128),
		"lines":   strings.Repeat("a", 127) + "\n",
		"unicode": "ab👩🏽‍💻é🇺🇳❤️Z\n",
		"ansi":    "\x1b[31mcolored\x1b[0m ordinary\n",
	} {
		b.Run(name, func(b *testing.B) {
			data := strings.Repeat(unit, (4<<20)/len(unit))
			source := benchmarkSource{strings.NewReader(data)}
			b.ReportAllocs()
			b.SetBytes(int64(len(data)))
			b.ResetTimer()
			for range b.N {
				text := NewText(source)
				if err := text.Update(100, true); err != nil {
					b.Fatal(err)
				}
				if err := text.Close(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkTextFollowAppend(b *testing.B) {
	part := []byte(strings.Repeat("a", 64<<10))
	doc := pager.NewDocument()
	text := NewText(doc)
	defer func() { text.Close(); doc.Close() }()
	b.ReportAllocs()
	b.SetBytes(int64(len(part)))
	b.ResetTimer()
	for range b.N {
		if doc.Len() >= 16<<20 {
			text.Close()
			doc.Close()
			doc = pager.NewDocument()
			text = NewText(doc)
		}
		if _, err := doc.Write(part); err != nil {
			b.Fatal(err)
		}
		if err := text.Update(100, false); err != nil {
			b.Fatal(err)
		}
		if _, err := text.Lines(max(0, text.RowCount()-24), 24); err != nil {
			b.Fatal(err)
		}
	}
}
