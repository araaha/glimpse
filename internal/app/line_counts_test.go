package app

import (
	"context"
	"testing"

	"glimpse/internal/pager"
	"glimpse/internal/ui"
)

func TestLineCountsMatchTheIndex(t *testing.T) {
	for _, input := range []string{
		"", "a", "a\n", "a\nb", "\n\n", "a\n\x1b[m", "a\x1b]0;t\nb\x07\nc",
		"x\x1b\ny", "\x1b[31mred\x1b[m\n\x1b[1mbold\x1b[m\n",
	} {
		doc := pager.NewDocument()
		doc.Write([]byte(input))
		text := ui.NewText(doc)
		if err := text.Update(10, true); err != nil {
			t.Fatal(err)
		}
		counts := newLineCounts()
		key := countKey{end: doc.Len()}
		counts.request(context.Background(), doc, key)
		if !counts.accept(<-counts.results) {
			t.Fatal("result rejected")
		}
		if got, ok := counts.total(key); !ok || got != int64(text.LineCount()) {
			t.Errorf("%q: %d lines (%v), index has %d", input, got, ok, text.LineCount())
		}
		counts.close()
		text.Close()
		doc.Close()
	}
}

func TestLineCountsReuseChunksAsInputGrows(t *testing.T) {
	doc := pager.NewDocument()
	defer doc.Close()
	counts := newLineCounts()
	defer counts.close()
	for i, appended := range []string{"a", "\nb", "\nc"} {
		doc.Write([]byte(appended))
		want := int64(i + 1)
		key := countKey{end: doc.Len()}
		counts.request(context.Background(), doc, key)
		counts.accept(<-counts.results)
		if got, ok := counts.total(key); !ok || got != want {
			t.Fatalf("step %d: %d (%v), want %d", i, got, ok, want)
		}
		if counts.request(context.Background(), doc, key); counts.running {
			t.Fatal("recounted an unchanged snapshot")
		}
	}
}
