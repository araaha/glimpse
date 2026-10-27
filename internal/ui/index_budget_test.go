package ui

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"glimpse/internal/pager"
)

func TestIndexBudgetYieldsAndEventuallyMatchesFullIndex(t *testing.T) {
	data := strings.Repeat("abcdefghij", 200000) + "👩🏽‍💻\x1b[31mend\x1b[0m"
	source := benchmarkSource{strings.NewReader(data)}
	text, _ := NewTextWithLimits(source, 16<<10, 16<<20)
	defer text.Close()
	if err := text.UpdateFor(80, true, time.Nanosecond); err != nil {
		t.Fatal(err)
	}
	if text.Indexed() <= 0 || text.Indexed() >= source.Len() || !text.NeedsUpdate(80, true) {
		t.Fatalf("index did not yield: %d/%d", text.Indexed(), source.Len())
	}
	if lines, err := text.Lines(0, 24); err != nil || len(lines) != 24 {
		t.Fatalf("partial viewport unavailable: %d, %v", len(lines), err)
	}
	for steps := 0; text.NeedsUpdate(80, true); steps++ {
		if steps > 10000 {
			t.Fatal("index stopped progressing")
		}
		if err := text.UpdateFor(80, true, time.Nanosecond); err != nil {
			t.Fatal(err)
		}
	}
	full, _ := NewTextWithLimits(source, 16<<10, 16<<20)
	defer full.Close()
	if err := full.Update(80, true); err != nil {
		t.Fatal(err)
	}
	if text.RowCount() != full.RowCount() {
		t.Fatalf("row counts differ: %d/%d", text.RowCount(), full.RowCount())
	}
	for _, top := range []int{0, text.RowCount() - 24} {
		got, _ := text.Lines(top, 24)
		want, _ := full.Lines(top, 24)
		if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(text.Styled(), full.Styled()) {
			t.Fatalf("budget changed viewport at row %d", top)
		}
	}
}

func TestIncompleteLiveTokenDoesNotSpinIndexWork(t *testing.T) {
	for _, suffix := range []string{"\xe7", "\x1b[38;2;"} {
		doc := pager.NewDocument()
		defer doc.Close()
		doc.Write([]byte("a" + suffix))
		text, _ := NewTextWithLimits(doc, 16<<10, 1<<20)
		defer text.Close()
		if err := text.UpdateFor(10, false, time.Millisecond); err != nil {
			t.Fatal(err)
		}
		if text.NeedsUpdate(10, false) {
			t.Fatalf("incomplete suffix %q schedules busy work", suffix)
		}
		if !text.NeedsUpdate(10, true) {
			t.Fatal("EOF did not schedule finalization")
		}
		if err := text.UpdateFor(10, true, time.Millisecond); err != nil {
			t.Fatal(err)
		}
		if text.NeedsUpdate(10, true) {
			t.Fatal("completed EOF still schedules work")
		}
	}
}

func TestBudgetedIndexReflowRetainsUnicodeStyles(t *testing.T) {
	data := strings.Repeat("a👩🏽‍💻é\x1b[4;58:5:196m❤️\x1b[0m\tend\n", 300)
	source := benchmarkSource{strings.NewReader(data)}
	text, _ := NewTextWithLimits(source, 16<<10, 4<<20)
	defer text.Close()
	for _, width := range []int{80, 9, 17} {
		for text.NeedsUpdate(width, true) {
			if err := text.UpdateFor(width, true, time.Nanosecond); err != nil {
				t.Fatal(err)
			}
		}
		full, _ := NewTextWithLimits(source, 16<<10, 4<<20)
		full.Update(width, true)
		if text.RowCount() != full.RowCount() {
			t.Fatal("budgeted reflow changed row count")
		}
		for _, top := range []int{0, text.RowCount() - 10} {
			got, _ := text.Lines(top, 10)
			want, _ := full.Lines(top, 10)
			if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(text.Styled(), full.Styled()) {
				t.Fatalf("width %d row %d changed styles", width, top)
			}
		}
		full.Close()
	}
}
