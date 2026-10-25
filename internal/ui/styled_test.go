package ui

import (
	"glimpse/internal/content"
	"glimpse/internal/pager"
	"reflect"
	"strings"
	"testing"
)

func TestCarriageReturnBackspaceAndManOverstrikes(t *testing.T) {
	for _, tt := range []struct {
		input, want string
		attribute   content.Style
	}{
		{"abc\rXY", "XYc", 0}, {"ab\bX", "aX", 0}, {"x\bx", "x", content.Bold},
		{"_\bU", "U", content.Underline}, {"U\b_", "U", content.Underline}, {"界\bX", "X ", 0},
		{"界\b_", "界", content.Underline},
	} {
		doc := pager.NewDocument()
		doc.Write([]byte(tt.input))
		text := NewText(doc)
		if err := text.Update(10, true); err != nil {
			t.Fatal(err)
		}
		lines, err := text.Lines(0, 10)
		if err != nil || !reflect.DeepEqual(lines, []string{tt.want}) {
			t.Fatalf("%q -> %q, %v", tt.input, lines, err)
		}
		if tt.attribute != 0 && text.Styled()[0][0].Style&tt.attribute == 0 {
			t.Fatalf("%q lost attribute", tt.input)
		}
		text.Close()
		doc.Close()
	}
}

func TestStyleCheckpointsSurviveWrappingAndResize(t *testing.T) {
	doc := pager.NewDocument()
	defer doc.Close()
	doc.Write([]byte("\x1b[31mabcdef\ngh\x1b[0mi"))
	text := NewText(doc)
	defer text.Close()
	for _, width := range []int{3, 2, 8} {
		if err := text.Update(width, true); err != nil {
			t.Fatal(err)
		}
		lines, err := text.Lines(0, 20)
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range text.Styled() {
			for _, glyph := range row {
				_, colored := glyph.Style.Foreground()
				if glyph.Text != "i" && !colored || glyph.Text == "i" && colored {
					t.Fatalf("width %d: style for %q in %q", width, glyph.Text, lines)
				}
			}
		}
	}
}

func TestRollingEvictionPreservesPreviouslyIndexedStyle(t *testing.T) {
	doc, _ := pager.NewDocumentWithOptions(16, pager.StorageOptions{MemoryBudget: 64, SpoolQuota: 128, Retention: "rolling"})
	defer doc.Close()
	text := NewText(doc)
	defer text.Close()
	doc.Write([]byte("\x1b[31m" + strings.Repeat("a", 32)))
	if err := text.Update(8, false); err != nil {
		t.Fatal(err)
	}
	doc.Write([]byte(strings.Repeat("b", 40)))
	for _, width := range []int{8, 5, 10} {
		if err := text.Update(width, false); err != nil {
			t.Fatal(err)
		}
		if _, err := text.Lines(0, 100); err != nil {
			t.Fatal(err)
		}
		for _, row := range text.Styled() {
			for _, glyph := range row {
				if _, ok := glyph.Style.Foreground(); !ok {
					t.Fatalf("lost evicted style at width %d", width)
				}
			}
		}
	}
	doc.Write([]byte("\x1b[0mc"))
	if err := text.Update(10, true); err != nil {
		t.Fatal(err)
	}
	text.Lines(0, 100)
	for _, row := range text.Styled() {
		for _, glyph := range row {
			_, colored := glyph.Style.Foreground()
			if glyph.Text == "c" && colored {
				t.Fatal("reset lost after eviction")
			}
		}
	}
}
