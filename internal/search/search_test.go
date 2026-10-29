package search

import (
	"context"
	"errors"
	"strings"
	"testing"

	"glimpse/internal/pager"
)

func TestFind(t *testing.T) {
	for _, tt := range []struct {
		name, input, query       string
		origin                   int64
		backward, smart, include bool
		want                     Result
	}{
		{"forward", "one two one", "one", 0, false, false, false, Result{8, true, false}},
		{"include origin", "one two one", "one", 0, false, false, true, Result{0, true, false}},
		{"forward wrap", "one two one", "one", 8, false, false, false, Result{0, true, true}},
		{"backward", "one two one", "one", 8, true, false, false, Result{0, true, false}},
		{"backward wrap", "one two one", "one", 0, true, false, false, Result{8, true, true}},
		{"backward include", "one two one", "one", 8, true, false, true, Result{8, true, false}},
		{"overlap", "ababa", "aba", 0, false, false, false, Result{2, true, false}},
		{"single match wraps", "abc", "abc", 0, false, false, false, Result{0, true, true}},
		{"sensitive default", "ABC abc", "abc", 0, false, false, true, Result{4, true, false}},
		{"smart insensitive", "ABC abc", "abc", 0, false, true, true, Result{0, true, false}},
		{"smart uppercase", "abc ABC", "ABC", 0, false, true, true, Result{4, true, false}},
		{"unicode offsets", "界x σΣ", "Σ", 0, false, false, true, Result{7, true, false}},
		{"unicode fold", "界x ςΣ", "σ", 0, false, true, true, Result{5, true, false}},
		{"unicode uppercase", "é É", "É", 0, false, true, true, Result{3, true, false}},
		{"literal punctuation", "a.b axb", "a.b", 0, false, false, true, Result{0, true, false}},
		{"cross newline", "ab\ncd", "b\nc", 0, false, false, true, Result{1, true, false}},
		{"not found", "abc", "z", 0, false, false, true, Result{}},
		{"empty input", "", "z", 0, false, false, true, Result{}},
		{"empty query", "abc", "", 0, false, false, true, Result{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			doc := pager.NewDocumentWithChunkSize(2)
			doc.Write([]byte(tt.input))
			got, err := Find(context.Background(), doc, literal(tt.query, tt.smart, false), tt.origin, tt.backward, tt.include)
			if got != tt.want || err != nil {
				t.Fatalf("Find = (%+v, %v), want %+v", got, err, tt.want)
			}
		})
	}
}

func TestFindLongLineAndBufferBoundary(t *testing.T) {
	doc := pager.NewDocumentWithChunkSize(7)
	doc.Write([]byte(strings.Repeat("a", 65535) + "界TARGET" + strings.Repeat("b", 65536)))
	got, err := Find(context.Background(), doc, literal("界target", true, false), 0, false, true)
	if err != nil || !got.Found || got.Offset != 65535 {
		t.Fatalf("Find = (%+v, %v)", got, err)
	}
}

type brokenSource struct{}

func (brokenSource) Len() int64                        { return 10 }
func (brokenSource) ReadAt([]byte, int64) (int, error) { return 0, errors.New("read failed") }

func TestFindErrorsAndCancellation(t *testing.T) {
	if _, err := Find(context.Background(), brokenSource{}, literal("x", false, false), 0, false, true); err == nil {
		t.Fatal("read error ignored")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Find(ctx, brokenSource{}, literal("x", false, false), 0, false, true); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled search: %v", err)
	}
}

type cancelingSource struct {
	*pager.Document
	cancel context.CancelFunc
}

func (s cancelingSource) ReadAt(p []byte, offset int64) (int, error) {
	n, err := s.Document.ReadAt(p, offset)
	s.cancel()
	return n, err
}

func TestFindCancellationDuringScan(t *testing.T) {
	doc := pager.NewDocument()
	doc.Write([]byte(strings.Repeat("x", 128*1024)))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err := Find(ctx, cancelingSource{doc, cancel}, literal("absent", false, false), 0, true, true)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("scan cancellation = %v", err)
	}
}

func TestIgnoreCase(t *testing.T) {
	doc := pager.NewDocument()
	doc.Write([]byte("abc σΣ ABC"))
	for _, tt := range []struct {
		query  string
		smart  bool
		offset int64
	}{
		{"ABC", false, 0},
		{"Σ", false, 4},
		{"ABC", true, 9},
	} {
		got, err := Find(context.Background(), doc, literal(tt.query, tt.smart, true), 0, false, true)
		if err != nil || !got.Found || got.Offset != tt.offset {
			t.Fatalf("query %q, smart %v: result %+v, error %v", tt.query, tt.smart, got, err)
		}
	}
}

func TestSearchSkipsSupportedAndUnsupportedANSI(t *testing.T) {
	doc := pager.NewDocument()
	defer doc.Close()
	doc.Write([]byte("pre\x1b[31mnee\x1b[0mdle\x1b]52;secret\a end"))
	got, err := Find(context.Background(), doc, literal("needle", false, false), 0, false, true)
	if err != nil || !got.Found || got.Offset != 8 {
		t.Fatalf("styled search = %+v, %v", got, err)
	}
	got, err = Find(context.Background(), doc, literal("secret", false, false), 0, false, true)
	if err != nil || got.Found {
		t.Fatalf("control string was searchable: %+v, %v", got, err)
	}
}

func TestSearchNormalizesManOverstrikes(t *testing.T) {
	doc := pager.NewDocument()
	defer doc.Close()
	doc.Write([]byte("w\bwo\bor\brd\bd _\bu\n"))
	for _, query := range []string{"word", "u"} {
		got, err := Find(context.Background(), doc, literal(query, false, false), 0, false, true)
		if err != nil || !got.Found {
			t.Fatalf("query %q: %+v, %v", query, got, err)
		}
	}
	got, err := Find(context.Background(), doc, literal("_", false, false), 0, false, true)
	if err != nil || got.Found {
		t.Fatalf("underline marker is not visible text: %+v %v", got, err)
	}
}

// literal is a query matched as typed, as with :set nomagic.
func literal(text string, smart, ignore bool) Query {
	return Query{Text: text, SmartCase: smart, IgnoreCase: ignore, Literal: true}
}
