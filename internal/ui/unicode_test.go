package ui

import (
	"glimpse/internal/pager"
	"reflect"
	"strings"
	"testing"
)

func TestGraphemeWrapping(t *testing.T) {
	for _, tt := range []struct {
		input string
		width int
		want  []string
	}{
		{"ab👨‍👩‍👧‍👦c", 4, []string{"ab👨‍👩‍👧‍👦", "c"}},
		{"ab👨‍👩‍👧‍👦c", 3, []string{"ab", "👨‍👩‍👧‍👦c"}},
		{"🇺🇳🇨🇦x", 2, []string{"🇺🇳", "🇨🇦", "x"}},
		{"👍🏽x", 2, []string{"👍🏽", "x"}},
		{"1️⃣x", 2, []string{"1️⃣", "x"}},
		{"a❤️x", 2, []string{"a", "❤️", "x"}},
		{"éx", 1, []string{"é", "x"}},
		{"👨‍👩‍👧‍👦x", 1, []string{"�", "x"}},
	} {
		doc := pager.NewDocument()
		doc.Write([]byte(tt.input))
		text, _ := NewTextWithLimits(doc, 16<<10, 1<<20)
		if err := text.Update(tt.width, true); err != nil {
			t.Fatal(err)
		}
		got, err := text.Lines(0, 100)
		if err != nil || !reflect.DeepEqual(got, tt.want) {
			t.Fatalf("%q width %d: %q, %v; want %q", tt.input, tt.width, got, err, tt.want)
		}
		text.Close()
		doc.Close()
	}
}

func TestEveryUnicodeByteSplitAndResize(t *testing.T) {
	input := "a👩🏽‍💻é🇺🇳❤️Z"
	for split := 0; split <= len(input); split++ {
		doc := pager.NewDocumentWithChunkSize(3)
		text, _ := NewTextWithLimits(doc, 16<<10, 1<<20)
		doc.Write([]byte(input[:split]))
		if err := text.Update(3, false); err != nil {
			t.Fatal(err)
		}
		text.Lines(0, 100)
		doc.Write([]byte(input[split:]))
		if err := text.Update(3, true); err != nil {
			t.Fatal(err)
		}
		got, err := text.Lines(0, 100)
		want := []string{"a👩🏽‍💻", "é🇺🇳", "❤️Z"}
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("split %d: %q, %v", split, got, err)
		}
		if err := text.Update(2, true); err != nil {
			t.Fatal(err)
		}
		got, _ = text.Lines(0, 100)
		if strings.Join(got, "") != input {
			t.Fatalf("resize split %d changed clusters: %q", split, got)
		}
		text.Close()
		doc.Close()
	}
}

func TestLiveVariationSelectorReflowsProvisionalCluster(t *testing.T) {
	doc := pager.NewDocument()
	defer doc.Close()
	text, _ := NewTextWithLimits(doc, 16<<10, 1<<20)
	defer text.Close()
	doc.Write([]byte("a❤"))
	if err := text.Update(2, false); err != nil {
		t.Fatal(err)
	}
	lines, _ := text.Lines(0, 100)
	if !reflect.DeepEqual(lines, []string{"a❤"}) {
		t.Fatalf("provisional rows = %q", lines)
	}
	doc.Write([]byte("\ufe0f"))
	if err := text.Update(2, false); err != nil {
		t.Fatal(err)
	}
	lines, _ = text.Lines(0, 100)
	if !reflect.DeepEqual(lines, []string{"a", "❤️"}) {
		t.Fatalf("extended cluster did not reflow: %q", lines)
	}
	if err := text.Update(2, false); err != nil {
		t.Fatal(err)
	}
	if text.RowCount() != 2 {
		t.Fatal("unchanged update duplicated rows")
	}
}

func TestAmbiguousWidthPolicy(t *testing.T) {
	doc := pager.NewDocument()
	defer doc.Close()
	doc.Write([]byte("·x"))
	text, _ := NewTextWithLimits(doc, 16<<10, 1<<20)
	defer text.Close()
	text.Update(2, true)
	narrow, _ := text.Lines(0, 100)
	text.SetWideAmbiguous(true)
	text.Update(2, true)
	wide, _ := text.Lines(0, 100)
	if !reflect.DeepEqual(narrow, []string{"·x"}) || !reflect.DeepEqual(wide, []string{"·", "x"}) {
		t.Fatalf("narrow/wide = %q/%q", narrow, wide)
	}
}
