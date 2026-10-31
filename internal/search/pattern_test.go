package search

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math/rand"
	"regexp"
	"strings"
	"testing"

	"github.com/araaha/glimpse/internal/content"
)

func TestQueryTranslate(t *testing.T) {
	for _, tt := range []struct {
		query     Query
		source    string
		fold, re  bool
		wantError bool
	}{
		{Query{Text: "abc"}, "abc", false, false, false},
		{Query{Text: "a.c"}, "a.c", false, true, false},
		{Query{Text: `a\.c`}, `a\.c`, false, false, false},
		{Query{Text: "a.c", Literal: true}, `a\.c`, false, false, false},
		{Query{Text: `x\Va.c*`}, `xa\.c\*`, false, false, false},
		{Query{Text: `a+\Vb+`}, `a+b\+`, false, true, false},
		{Query{Text: `\V\\a\b`}, `\\a\\b`, false, false, false},
		{Query{Text: `a\`, Literal: true}, `a\\`, false, false, false},
		{Query{Text: `foo\c`}, "foo", true, false, false},
		{Query{Text: `\CFoo`, SmartCase: true}, "Foo", false, false, false},
		{Query{Text: `\C\cFoo`}, "Foo", true, false, false},
		{Query{Text: "Foo", SmartCase: true}, "Foo", false, false, false},
		{Query{Text: "foo", SmartCase: true}, "foo", true, false, false},
		{Query{Text: `\Sfoo\W`, SmartCase: true}, `\Sfoo\W`, true, true, false},
		{Query{Text: `\p{Lu}x`, SmartCase: true}, `\p{Lu}x`, true, true, false},
		{Query{Text: `\VS`, SmartCase: true}, "S", false, false, false},
		{Query{Text: "Foo", IgnoreCase: true}, "Foo", true, false, false},
		{Query{Text: `\c`}, "", true, false, false},
		{Query{Text: "a("}, "a(", false, false, true},
		{Query{Text: `a\`}, `a\`, false, false, true},
		{Query{Text: `(?<=a)b`}, `(?<=a)b`, false, false, true},
	} {
		source, fold := tt.query.translate()
		if source != tt.source || fold != tt.fold {
			t.Errorf("%+v: translate = %q, %v; want %q, %v", tt.query, source, fold, tt.source, tt.fold)
		}
		err := tt.query.Validate()
		if (err != nil) != tt.wantError {
			t.Errorf("%+v: Validate = %v", tt.query, err)
		}
		if err == nil && tt.query.IsRegex() != tt.re {
			t.Errorf("%+v: IsRegex = %v, want %v", tt.query, !tt.re, tt.re)
		}
	}
	if err := (Query{Text: "a("}).Validate(); err == nil || err.Error() != "invalid pattern: missing closing )" {
		t.Errorf("error message = %v", err)
	}
}

// regexMatches walks a query over input and returns "start-end" pairs.
func regexMatches(t testing.TB, input string, query Query) []string {
	t.Helper()
	source := benchmarkSource{strings.NewReader(input)}
	var got []string
	err := Walk(context.Background(), source, query, 0, source.Len(), func(start, end int64) bool {
		got = append(got, fmt.Sprintf("%d-%d", start, end))
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestRegexWalk(t *testing.T) {
	for _, tt := range []struct {
		input string
		query Query
		want  string
	}{
		{"foo bar\nbar foo", Query{Text: "^bar"}, "8-11"},
		{"foo bar\nbar foo", Query{Text: "bar$"}, "4-7"},
		{"foo bar\r\nbar", Query{Text: "bar$"}, "4-7 9-12"},
		{"ab\ncd", Query{Text: `b\sc`}, ""},
		{"ab\ncd", Query{Text: `b.c`}, ""},
		{"aaaa ba", Query{Text: "a+"}, "0-4 6-7"},
		{"aaaa", Query{Text: "aa"}, "0-2 1-3 2-4"},
		{"xyz", Query{Text: "q*"}, ""},
		{"FOO foo", Query{Text: "f.o", SmartCase: true}, "0-3 4-7"},
		{"FOO foo", Query{Text: "F.O", SmartCase: true}, "0-3"},
		{"FOO foo", Query{Text: `\Cf.o`, IgnoreCase: true}, "4-7"},
		{"σΣς", Query{Text: "[σ]+", IgnoreCase: true}, "0-6"},
		{"a.c abc", Query{Text: `\Va.c`}, "0-3"},
		{"a1 b2 cat", Query{Text: `\b[a-z]\d\b`}, "0-2 3-5"},
		{"pre\x1b[31mnee\x1b[0mdle", Query{Text: "ne+dle"}, "8-18"},
		{"w\bwo\bor\brd\bd x", Query{Text: "wo.d"}, "0-12"},
		{"界x 界y", Query{Text: "界."}, "0-4 5-9"},
		{"a\nb\n", Query{Text: "^.$"}, "0-1 2-3"},
		// An unterminated \Q quotes the rest of the pattern.
		{"a.b a(x ax(", Query{Text: `a.\Q(`}, "8-11"},
		{"x$ y", Query{Text: `\w\Q$`}, "0-2"},
	} {
		if got := strings.Join(regexMatches(t, tt.input, tt.query), " "); got != tt.want {
			t.Errorf("%q in %q = %q, want %q", tt.query.Text, tt.input, got, tt.want)
		}
	}
}

// TestRegexLongLines checks the sliding window against matching whole lines,
// for matches far shorter than MaxMatchBytes placed around window seams.
func TestRegexLongLines(t *testing.T) {
	random := rand.New(rand.NewSource(1))
	var input strings.Builder
	for _, length := range []int{10, 2*MaxMatchBytes - 3, 5*MaxMatchBytes + 17, 40} {
		line := []byte(strings.Repeat("a", length))
		for range length / 997 {
			i := random.Intn(length - 1)
			line[i], line[i+1] = 'b', byte('0'+random.Intn(10))
		}
		for _, seam := range []int{MaxMatchBytes, 2 * MaxMatchBytes, 3 * MaxMatchBytes} {
			for _, at := range []int{seam - 2, seam - 1, seam, seam + 1} {
				if at+2 <= length {
					copy(line[at:], "b7")
				}
			}
		}
		input.Write(line)
		input.WriteByte('\n')
	}
	text := input.String()
	for _, pattern := range []string{`b\d`, `\bb\d`, `ab\d+a`, `^a`, `a$`, `b[0-9]a{1,3}`} {
		var want []string
		offset := 0
		for _, line := range strings.SplitAfter(text, "\n") {
			body := strings.TrimSuffix(line, "\n")
			for _, m := range regexp.MustCompile(pattern).FindAllStringIndex(body, -1) {
				want = append(want, fmt.Sprintf("%d-%d", offset+m[0], offset+m[1]))
			}
			offset += len(line)
		}
		got := regexMatches(t, text, Query{Text: pattern})
		if strings.Join(got, " ") != strings.Join(want, " ") {
			t.Errorf("%s: %d matches, want %d", pattern, len(got), len(want))
		}
	}
	// Matches longer than the window bound are only found at a line's end.
	long := strings.Repeat("x", 4*MaxMatchBytes) + "\n"
	if got := regexMatches(t, long, Query{Text: "x+"}); len(got) != 1 || !strings.HasSuffix(got[0], fmt.Sprintf("-%d", 4*MaxMatchBytes)) {
		t.Errorf("long match = %v", got)
	}
}

// TestRegexLongMultibyteLines repeats the whole-line comparison with runes of
// every UTF-8 length, so window cuts and resumes land inside runes, and with
// patterns that also match empty strings.
func TestRegexLongMultibyteLines(t *testing.T) {
	random := rand.New(rand.NewSource(2))
	runes := []string{"a", "b", " ", "é", "界", "😀", "ß"}
	var input strings.Builder
	for _, length := range []int{3*MaxMatchBytes + 5, 2 * MaxMatchBytes, 7} {
		for written := 0; written < length; {
			r := runes[random.Intn(len(runes))]
			input.WriteString(r)
			written += len(r)
		}
		input.WriteByte('\n')
	}
	text := input.String()
	for _, pattern := range []string{`界.`, `é+`, `a*`, `(?i)ß|É`, `\bb`, `[^a ]{2}`, `😀$`, `^.`} {
		var want []string
		offset := 0
		for _, line := range strings.SplitAfter(text, "\n") {
			body := strings.TrimSuffix(line, "\n")
			for _, m := range regexp.MustCompile(pattern).FindAllStringIndex(body, -1) {
				if m[0] < m[1] {
					want = append(want, fmt.Sprintf("%d-%d", offset+m[0], offset+m[1]))
				}
			}
			offset += len(line)
		}
		got := regexMatches(t, text, Query{Text: pattern})
		if strings.Join(got, " ") != strings.Join(want, " ") {
			for i := range min(len(got), len(want)) {
				if got[i] != want[i] {
					t.Errorf("%s: match %d = %s, want %s", pattern, i, got[i], want[i])
					break
				}
			}
			t.Errorf("%s: %d matches, want %d", pattern, len(got), len(want))
		}
	}
}

func TestRegexChunks(t *testing.T) {
	var input strings.Builder
	for i := range 400 {
		fmt.Fprintf(&input, "line %d: \x1b[1mid=%03d\x1b[0m value %d\n", i, i*7%1000, i*i)
	}
	text := input.String()
	query := Query{Text: `^line \d*7:|=0\d+|value \d{4}$`}
	want := regexMatches(t, text, query)
	source := benchmarkSource{strings.NewReader(text)}
	for _, size := range []int64{1, 50, 4096} {
		chunks, err := UpdateChunks(context.Background(), source, Chunks{}, 0, source.Len(), Options{Query: query, ChunkSize: size, Workers: 3})
		if err != nil || chunks.Total() != int64(len(want)) {
			t.Fatalf("size %d: total %d, %v; want %d", size, chunks.Total(), err, len(want))
		}
		for _, backward := range []bool{false, true} {
			got, err := FindInChunks(context.Background(), source, query, chunks, 1000, 0, backward, false)
			var starts []int64
			for _, match := range want {
				var start, end int64
				fmt.Sscanf(match, "%d-%d", &start, &end)
				starts = append(starts, start)
			}
			wantResult := Result{Found: true}
			for _, start := range starts {
				if !backward && start > 1000 {
					wantResult.Offset = start
					break
				}
				if backward && start < 1000 {
					wantResult.Offset = start
				}
			}
			if err != nil || got != wantResult {
				t.Fatalf("size %d backward %v: %+v, %v; want %+v", size, backward, got, err, wantResult)
			}
			got, err = FindParallel(context.Background(), source, query, 1000, 0, backward, false, Options{ChunkSize: size, Workers: 3})
			if err != nil || got != wantResult {
				t.Fatalf("parallel size %d backward %v: %+v, %v; want %+v", size, backward, got, err, wantResult)
			}
		}
	}
}

// TestNewlinesMatchDecoder checks the newline fast path against decoding every
// token, including newlines hidden in or ending malformed escapes.
func TestNewlinesMatchDecoder(t *testing.T) {
	for _, input := range []string{
		"", "a", "a\n", "a\nb\n\n",
		"a\x1b]0;title\nmore\x07\nb\n",
		"x\x1b\ny\n",
		"\x1b[1\n2m\n",
		"\xc2\x9d0;t\n\xc2\x9c\n",
		"\x1b(\nB\n",
		"©\n界\n\xc2\n\xe2\n",
		"a\b\n_\b\nb\n",
		"\x1b[31mred\x1b[m\n\x1b[1mbold\x1b[m\n",
		"ok\n\x1b]unterminated\n\n",
	} {
		source := benchmarkSource{strings.NewReader(input)}
		var want []string
		reader := content.NewPlainReader(source, 0, source.Len())
		for {
			token, err := reader.Next()
			if err != nil {
				break
			}
			if !token.Hidden && token.Raw == '\n' {
				want = append(want, fmt.Sprintf("%d-%d", token.Start, token.End))
			}
		}
		got := regexMatches(t, input, Query{Text: "\n", Literal: true})
		if strings.Join(got, " ") != strings.Join(want, " ") {
			t.Errorf("%q: newlines %v, want %v", input, got, want)
		}
		for _, size := range []int64{1, 3} {
			chunks, err := UpdateChunks(context.Background(), source, Chunks{}, 0, source.Len(), Options{Query: Query{Text: "\n", Literal: true}, ChunkSize: size, Workers: 2})
			if err != nil || chunks.Total() != int64(len(want)) {
				t.Errorf("%q size %d: total %d, %v; want %d", input, size, chunks.Total(), err, len(want))
			}
		}
	}
}

// A query holding U+FFFD, typed or decoded from invalid UTF-8, is still a
// literal: its matches overlap like any other literal's.
func TestReplacementCharacterLiteral(t *testing.T) {
	input := "\xce\xce\xce a��b\xff\n"
	for _, tt := range []struct {
		query Query
		re    bool
	}{
		{literal("��", false, false), false},
		{literal("\xff\x80", false, false), false},
		{literal("A�", true, false), false},
		{Query{Text: "a�"}, false},
		{Query{Text: "�+"}, true},
	} {
		if got := tt.query.IsRegex(); got != tt.re {
			t.Errorf("%q IsRegex = %v, want %v", tt.query.Text, got, tt.re)
		}
		if !tt.re {
			checkFastSearch(t, input, tt.query.Text, tt.query.SmartCase, tt.query.IgnoreCase)
		}
	}
	if (Query{Text: `\V\x{FFFD}.` + "\uFFFD"}).IsRegex() {
		t.Error(`\V query with U+FFFD is a regex`)
	}
	source := benchmarkSource{strings.NewReader(input)}
	var got []matchRange
	if err := Walk(context.Background(), source, Query{Text: "�+"}, 0, source.Len(), func(start, end int64) bool {
		got = append(got, matchRange{start, end})
		return true
	}); err != nil {
		t.Fatal(err)
	}
	if want := []matchRange{{0, 3}, {5, 11}, {12, 13}}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("regex matches %v, want %v", got, want)
	}
}

// referenceRegex runs Go's regexp over each decoded line in one piece, without
// the windows, ASCII batching, or resume patterns of walkRegex.
func referenceRegex(t testing.TB, input string, re *regexp.Regexp) []string {
	t.Helper()
	reader := searchReader{reader: content.NewReader(strings.NewReader(input), 0, int64(len(input)), 0, true), ctx: context.Background()}
	var got []string
	var text []byte
	var starts, ends []int64
	flush := func() {
		body := bytes.TrimSuffix(text, []byte{'\r'})
		for _, m := range re.FindAllIndex(body, -1) {
			if m[0] < m[1] {
				got = append(got, fmt.Sprintf("%d-%d", starts[m[0]], ends[m[1]-1]))
			}
		}
		text, starts, ends = text[:0], starts[:0], ends[:0]
	}
	for {
		token, err := reader.next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if token.Hidden {
			continue
		}
		if token.Raw == '\n' {
			flush()
			continue
		}
		encoded := string(token.Raw)
		text = append(text, encoded...)
		for range len(encoded) {
			starts, ends = append(starts, token.Start), append(ends, token.End)
		}
	}
	flush()
	return got
}

func FuzzRegex(f *testing.F) {
	f.Add("foo bar\r\nbar foo\n", "^bar|o+$", false)
	f.Add("pre\x1b[31mnee\x1b[0mdle w\bwo\bor\brd\bd", `n.e|w\w`, true)
	f.Add("界x 界y\nσΣς\xff", `[σ界].|\pL\b`, true)
	f.Fuzz(func(t *testing.T, input, pattern string, ignore bool) {
		if len(input) > 4096 || len(pattern) > 32 {
			t.Skip()
		}
		query := Query{Text: pattern, IgnoreCase: ignore}
		// Escapes that change the translation, and plain literals (searched
		// with overlapping KMP matches), are covered by other tests.
		if strings.Contains(pattern, `\V`) || strings.Contains(pattern, `\c`) || strings.Contains(pattern, `\C`) || !query.IsRegex() {
			t.Skip()
		}
		flags := ""
		if ignore {
			flags = "(?i)"
		}
		// The query is read as runes, so invalid UTF-8 becomes U+FFFD.
		want := referenceRegex(t, input, regexp.MustCompile(flags+string([]rune(pattern))))
		if got := regexMatches(t, input, query); strings.Join(got, " ") != strings.Join(want, " ") {
			t.Fatalf("%q in %q = %v, want %v", pattern, input, got, want)
		}
	})
}
