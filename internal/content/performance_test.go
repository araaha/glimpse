package content

import (
	"io"
	"strings"
	"testing"
)

func BenchmarkGraphemeASCII(b *testing.B) {
	data := strings.Repeat("a", 1<<20)
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for range b.N {
		reader := NewGraphemeReader(strings.NewReader(data), 0, int64(len(data)), 0, true)
		for {
			_, err := reader.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				b.Fatal(err)
			}
		}
	}
}
