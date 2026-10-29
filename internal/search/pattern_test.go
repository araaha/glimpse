package search

import (
	"context"
	"fmt"
	"math/rand"
	"regexp"
	"strings"
	"testing"
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
		}
	}
}
