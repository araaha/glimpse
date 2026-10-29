package search

import (
	"context"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
	"unicode"

	"glimpse/internal/content"
)

type guardedSource struct {
	benchmarkSource
	minimum int64
	read    int
}

func (s *guardedSource) ReadAt(p []byte, offset int64) (int, error) {
	if offset < s.minimum {
		return 0, fmt.Errorf("unnecessary prefix read at %d", offset)
	}
	n, err := s.benchmarkSource.ReadAt(p, offset)
	s.read += n
	return n, err
}

func TestAnchoredSearchSkipsPrefixAndUnneededSuffix(t *testing.T) {
	prefix := strings.Repeat("prefix\n", 100000)
	for _, backward := range []bool{false, true} {
		input := prefix + "hit\norigin\nhit\n" + strings.Repeat("suffix\n", 100000)
		anchor := int64(len(prefix))
		source := &guardedSource{benchmarkSource: benchmarkSource{strings.NewReader(input)}, minimum: anchor}
		got, err := FindFrom(context.Background(), source, literal("hit", false, false), anchor+4, anchor, backward, false)
		want := anchor + 11
		if backward {
			want = anchor
		}
		if err != nil || !got.Found || got.Offset != want || got.Wrapped {
			t.Fatalf("backward %v result %+v, %v; want %d", backward, got, err, want)
		}
		if source.read > 8192 {
			t.Fatalf("nearby search read %d bytes", source.read)
		}
	}
}

type matchRange struct{ start, end int64 }

// Deliberately use only single-token decoding and a simple literal comparison,
// not the batching/KMP/range-selection code being tested.
func referenceMatches(t testing.TB, input, query string, smart, ignore bool) []matchRange {
	t.Helper()
	pattern := []rune(query)
	if len(pattern) == 0 {
		return nil
	}
	fold := smart || ignore
	for _, r := range pattern {
		if smart && unicode.IsUpper(r) {
			fold = false
		}
	}
	reader := searchReader{reader: content.NewReader(strings.NewReader(input), 0, int64(len(input)), 0, true), ctx: context.Background()}
	var tokens []content.Token
	for {
		token, err := reader.next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		tokens = append(tokens, token)
	}
	var matches []matchRange
	for i := 0; i+len(pattern) <= len(tokens); i++ {
		found := true
		for j, r := range pattern {
			got := tokens[i+j].Raw
			if fold {
				got, r = foldRune(got), foldRune(r)
			}
			if got != r {
				found = false
				break
			}
		}
		if found {
			matches = append(matches, matchRange{tokens[i].Start, tokens[i+len(pattern)-1].End})
		}
	}
	return matches
}

func referenceResult(matches []matchRange, origin int64, backward, include bool) Result {
	var best, wrapped Result
	for _, match := range matches {
		eligible := match.start > origin || include && match.start == origin
		if backward {
			eligible = match.start < origin || include && match.start == origin
		}
		if eligible {
			best = Result{Offset: match.start, Found: true}
			if !backward {
				return best
			}
		} else if backward || !wrapped.Found {
			wrapped = Result{Offset: match.start, Found: true, Wrapped: true}
		}
	}
	if best.Found {
		return best
	}
	return wrapped
}

func checkFastSearch(t testing.TB, input, query string, smart, ignore bool) {
	t.Helper()
	source := benchmarkSource{strings.NewReader(input)}
	want := referenceMatches(t, input, query, smart, ignore)
	var got []matchRange
	err := Walk(context.Background(), source, literal(query, smart, ignore), 0, source.Len(), func(start, end int64) bool {
		got = append(got, matchRange{start, end})
		return true
	})
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("Walk(%q, %q) = %v, %v; want %v", input, query, got, err, want)
	}
	// Decoded newlines, not raw bytes inside hidden strings, are safe anchors.
	anchors := []int64{0}
	r := content.NewPlainReader(source, 0, source.Len())
	for {
		token, err := r.Next()
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
	for _, origin := range []int64{0, source.Len() / 2, source.Len()} {
		anchor := int64(0)
		for _, offset := range anchors {
			if offset <= origin {
				anchor = offset
			}
		}
		for _, backward := range []bool{false, true} {
			for _, include := range []bool{false, true} {
				got, err := FindFrom(context.Background(), source, literal(query, smart, ignore), origin, anchor, backward, include)
				want := referenceResult(want, origin, backward, include)
				if got != want || err != nil {
					t.Fatalf("FindFrom origin %d anchor %d backward %v include %v = %+v, %v; want %+v", origin, anchor, backward, include, got, err, want)
				}
			}
		}
	}
}

func TestFastSearchMatchesReference(t *testing.T) {
	for _, input := range []string{
		"ababa\naba", "abc\x1b[31mde\x1b[mf", "abc\x1b[31m\b_c\bc\n",
		"a\ba_\bb\x1b[31m_\bcd\bd\n", "ſSσςΣKkK\nσ", "first\n\x1b]52;hidden\naba\aababa\nlast",
		strings.Repeat("x", 4093) + "abc\x1b[31mdef\x1b[m\nlast", "abc\xff\x00\x1b[31", "a\n\x1bPaba\nbab\x1b\\aba\n",
	} {
		for _, query := range []string{"a", "aba", "abcdef", "cd", "σ", "k", "s", "\n", "not present"} {
			for _, fold := range []bool{false, true} {
				checkFastSearch(t, input, query, false, fold)
			}
		}
	}
}

func FuzzFastSearch(f *testing.F) {
	f.Add("a\x1b[31mb\x1b[maba\nlast", "aba", true, false)
	f.Add("a\ba_\bb\nKkσς", "k", false, true)
	f.Fuzz(func(t *testing.T, input, query string, smart, ignore bool) {
		if len(input) > 8192 || len(query) > 64 {
			t.Skip()
		}
		checkFastSearch(t, input, query, smart, ignore)
	})
}
