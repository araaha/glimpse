package ui

import (
	"github.com/araaha/glimpse/internal/pager"
	"math/rand"
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

func TestStreamedNewlineStylesMatchOneShot(t *testing.T) {
	for _, code := range []string{"\x1b[31m", "\x1b[0m", "\x1b[38;2;1;2;3m"} {
		doc := pager.NewDocument()
		text, _ := NewTextWithLimits(doc, 16<<10, 1<<20)
		doc.Write([]byte("\x1b[32ma\n" + code))
		if err := text.Update(5, false); err != nil {
			t.Fatal(err)
		}
		doc.Write([]byte("b"))
		if err := text.Update(5, true); err != nil {
			t.Fatal(err)
		}
		text.Lines(0, 100)
		last := text.Styled()[1][0]
		freshDoc := pager.NewDocument()
		freshDoc.Write([]byte("\x1b[32ma\n" + code + "b"))
		fresh, _ := NewTextWithLimits(freshDoc, 16<<10, 1<<20)
		fresh.Update(5, true)
		fresh.Lines(0, 100)
		if last.Style != fresh.Styled()[1][0].Style {
			t.Fatalf("streamed %q style %x != one-shot %x", code, last.Style, fresh.Styled()[1][0].Style)
		}
		text.Close()
		fresh.Close()
		doc.Close()
		freshDoc.Close()
	}
}

func TestTabsAtFullRowsCreateCellsBeforeControlEdits(t *testing.T) {
	for _, tt := range []struct {
		input string
		want  []string
	}{
		{"ab\tx", []string{"ab", "  ", "x"}},
		{"ab\t\rx", []string{"ab", "x "}},
		{"ab\t\bXY", []string{"ab", " X", "Y"}},
	} {
		doc := pager.NewDocument()
		doc.Write([]byte(tt.input))
		text, _ := NewTextWithLimits(doc, 16<<10, 1<<20)
		text.Update(2, true)
		got, err := text.Lines(0, 100)
		if err != nil || !reflect.DeepEqual(got, tt.want) {
			t.Fatalf("%q -> %q, %v; want %q", tt.input, got, err, tt.want)
		}
		text.Close()
		doc.Close()
	}
}

func TestHiddenSuffixRenderingReadIsBounded(t *testing.T) {
	for _, leading := range []bool{false, true} {
		doc := pager.NewDocument()
		input := "x" + strings.Repeat("\x1b[0m", 20000)
		if leading {
			input = strings.Repeat("\x1b[0m", 20000) + "x"
		}
		doc.Write([]byte(input))
		source := &countedTextSource{Document: doc}
		text, _ := NewTextWithLimits(source, 16<<10, 1<<20)
		if err := text.Update(1, true); err != nil {
			t.Fatal(err)
		}
		source.readBytes = 0
		lines, err := text.Lines(0, 1)
		if err != nil || !reflect.DeepEqual(lines, []string{"x"}) {
			t.Fatalf("leading %v: %q %v", leading, lines, err)
		}
		if !leading && source.readBytes > 4096 {
			t.Fatalf("read unbounded hidden suffix: %d", source.readBytes)
		}
		text.Close()
		doc.Close()
	}
}

func TestDeterministicStreamLayoutMatchesOneShot(t *testing.T) {
	random := rand.New(rand.NewSource(42))
	atoms := []string{"a", "b", "\n", "\r", "\b", "\t", "\x1b[31m", "\x1b[0m", "👩‍💻", "́", "❤️"}
	for iteration := 0; iteration < 100; iteration++ {
		var input strings.Builder
		for range 20 {
			input.WriteString(atoms[random.Intn(len(atoms))])
		}
		data := input.String()
		width := 1 + random.Intn(6)
		doc := pager.NewDocument()
		text, _ := NewTextWithLimits(doc, 16<<10, 1<<20)
		for _, b := range []byte(data) {
			doc.Write([]byte{b})
			if err := text.Update(width, false); err != nil {
				t.Fatal(err)
			}
		}
		if err := text.Update(width, true); err != nil {
			t.Fatal(err)
		}
		streamed, err := text.Lines(0, 100)
		if err != nil {
			t.Fatal(err)
		}
		freshDoc := pager.NewDocument()
		freshDoc.Write([]byte(data))
		fresh, _ := NewTextWithLimits(freshDoc, 16<<10, 1<<20)
		fresh.Update(width, true)
		all, err := fresh.Lines(0, 100)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(streamed, all) || !reflect.DeepEqual(text.Styled(), fresh.Styled()) {
			t.Fatalf("iteration %d width %d input %q: stream %q != one-shot %q", iteration, width, data, streamed, all)
		}
		text.Close()
		fresh.Close()
		doc.Close()
		freshDoc.Close()
	}
}
