package search

import (
	"context"
	"io"
	"strings"
	"testing"

	"glimpse/internal/content"
)

// retainedSource is a snapshot whose bytes before start were discarded.
type retainedSource struct {
	benchmarkSource
	start int64
}

func (s retainedSource) Start() int64 { return s.start }

func shifted(matches []matchRange, by int64) []matchRange {
	out := make([]matchRange, len(matches))
	for i, match := range matches {
		out[i] = matchRange{match.start + by, match.end + by}
	}
	return out
}

func checkChunkCounts(t testing.TB, chunks Chunks, base, end int64, want []matchRange) {
	t.Helper()
	if chunks.Start() != base && end > base || chunks.End() != end && end > base || chunks.Total() != int64(len(want)) {
		t.Fatalf("chunks [%d,%d) total %d; want [%d,%d) total %d: %+v", chunks.Start(), chunks.End(), chunks.Total(), base, end, len(want), chunks.List)
	}
	for i, chunk := range chunks.List {
		if i > 0 && chunk.Start != chunks.List[i-1].End || chunk.Start >= chunk.End {
			t.Fatalf("chunks are not contiguous: %+v", chunks.List)
		}
		var count int64
		for _, match := range want {
			if match.start >= chunk.Start && match.start < chunk.End {
				count++
			}
		}
		if count != chunk.Count {
			t.Fatalf("chunk %+v counted %d, want %d (chunks %+v, matches %v)", chunk, chunk.Count, count, chunks.List, want)
		}
	}
}

func checkChunks(t testing.TB, input, query string, smart, ignore bool, size int64) {
	t.Helper()
	ctx := context.Background()
	options := Options{Query: query, SmartCase: smart, IgnoreCase: ignore, ChunkSize: size, Workers: 3}
	source := benchmarkSource{strings.NewReader(input)}
	want := referenceMatches(t, input, query, smart, ignore)
	chunks, err := UpdateChunks(ctx, source, Chunks{}, 0, source.Len(), options)
	if err != nil {
		t.Fatal(err)
	}
	checkChunkCounts(t, chunks, 0, source.Len(), want)

	// Appending reuses verified chunks and only rescans the open tail.
	for _, split := range []int{len(input) / 3, len(input) / 2, len(input) - 1} {
		if split <= 0 {
			continue
		}
		prefix := benchmarkSource{strings.NewReader(input[:split])}
		partial, err := UpdateChunks(ctx, prefix, Chunks{}, 0, int64(split), options)
		if err != nil {
			t.Fatal(err)
		}
		checkChunkCounts(t, partial, 0, int64(split), referenceMatches(t, input[:split], query, smart, ignore))
		grown, err := UpdateChunks(ctx, source, partial, 0, source.Len(), options)
		if err != nil {
			t.Fatal(err)
		}
		checkChunkCounts(t, grown, 0, source.Len(), want)
	}

	// Rolling retention decodes from the new base, reusing later chunks.
	for _, base := range []int{1, len(input) / 2, len(input) - 1} {
		if base <= 0 || base >= len(input) {
			continue
		}
		retained := retainedSource{source, int64(base)}
		moved, err := UpdateChunks(ctx, retained, chunks, int64(base), source.Len(), options)
		if err != nil {
			t.Fatal(err)
		}
		checkChunkCounts(t, moved, int64(base), source.Len(), shifted(referenceMatches(t, input[base:], query, smart, ignore), int64(base)))
	}

	anchors := []int64{0}
	reader := content.NewPlainReader(source, 0, source.Len())
	for {
		token, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if !token.Hidden && token.Raw == '\n' {
			anchors = append(anchors, token.End)
		}
	}
	for _, origin := range []int64{0, 1, source.Len() / 3, source.Len() / 2, source.Len() - 1, source.Len()} {
		origin = min(max(0, origin), source.Len())
		for _, anchor := range []int64{0, -1} {
			if anchor < 0 {
				for _, offset := range anchors {
					if offset <= origin {
						anchor = offset
					}
				}
			}
			for _, backward := range []bool{false, true} {
				for _, include := range []bool{false, true} {
					got, err := FindInChunks(ctx, source, query, chunks, origin, anchor, backward, smart, ignore, include)
					want := referenceResult(want, origin, backward, include)
					if got != want || err != nil {
						t.Fatalf("FindInChunks(%q, %q) origin %d anchor %d backward %v include %v = %+v, %v; want %+v (chunks %+v)", input, query, origin, anchor, backward, include, got, err, want, chunks.List)
					}
				}
			}
		}
	}
}

func TestChunksMatchReference(t *testing.T) {
	for _, input := range []string{
		"ababa\naba", "abc\x1b[31mde\x1b[mf\nabc\nabc", "abc\x1b[31m\b_c\bc\n",
		"a\ba_\bb\x1b[31m_\bcd\bd\n", "ſSσςΣKkK\nσ", "first\n\x1b]52;hidden\naba\aababa\nlast",
		strings.Repeat("x", 4093) + "abc\x1b[31mdef\x1b[m\nlast", "abc\xff\x00\x1b[31", "a\n\x1bPaba\nbab\x1b\\aba\n",
		"hit\nhit\n\nhit\n", "a\x1b]0;\n\b_\x07\bb\naba\n", strings.Repeat("aba\n\x1b]0;aba\naba\x07aba\n", 40),
	} {
		for _, query := range []string{"a", "aba", "abcdef", "cd", "σ", "k", "\n", "a\na", "not present"} {
			for _, size := range []int64{1, 2, 5, 16, DefaultChunkSize} {
				for _, fold := range []bool{false, true} {
					checkChunks(t, input, query, false, fold, size)
				}
			}
		}
	}
}

// A raw newline inside an OSC string is not a restart point: decoding from it
// would expose the hidden "hit". The chunk must merge with its successor.
func TestChunksRejectNewlineInsideHiddenString(t *testing.T) {
	input := "hit\n\x1b]0;x\nhit\x07hit\nhit\n"
	source := benchmarkSource{strings.NewReader(input)}
	chunks, err := UpdateChunks(context.Background(), source, Chunks{}, 0, source.Len(), Options{Query: "hit", ChunkSize: 1, Workers: 4})
	if err != nil {
		t.Fatal(err)
	}
	inside := int64(strings.Index(input, "x\n") + 2)
	for _, chunk := range chunks.List {
		if chunk.Start == inside {
			t.Fatalf("chunk starts inside an OSC string: %+v", chunks.List)
		}
	}
	if chunks.Total() != 3 {
		t.Fatalf("total = %d, want 3: %+v", chunks.Total(), chunks.List)
	}
}

func TestChunksCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	source := benchmarkSource{strings.NewReader(strings.Repeat("hit\n", 1000))}
	if _, err := UpdateChunks(ctx, source, Chunks{}, 0, source.Len(), Options{Query: "hit", ChunkSize: 64, Workers: 2}); err == nil {
		t.Fatal("canceled count succeeded")
	}
}

func FuzzChunks(f *testing.F) {
	f.Add("a\x1b]0;\nb\x07ab\naba\n", "ab", uint8(1), false)
	f.Add("a\ba_\bb\nKkσς\n", "k", uint8(3), true)
	f.Fuzz(func(t *testing.T, input, query string, size uint8, ignore bool) {
		if len(input) > 4096 || len(query) > 32 {
			t.Skip()
		}
		checkChunks(t, input, query, false, ignore, int64(size)+1)
	})
}
