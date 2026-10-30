package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func reflowInput(osc bool) string {
	var b strings.Builder
	for i := 0; b.Len() < 5<<20; i++ {
		switch i % 7 {
		case 0:
			fmt.Fprintf(&b, "\x1b[3%dmcolored line %d continues", i%8, i) // style spans lines
		case 1:
			b.WriteString("漢字かなカナ한글ＡＢ mixed text\x1b[0m")
		case 2:
			b.WriteString("tab\tseparated\tcolumns\tb\bbold_\bunder")
		case 3:
			b.WriteString(strings.Repeat("long ", 300))
		case 4:
			b.WriteString("e\u0301 combining 👩🏽‍💻 emoji \x1b[4:3;58;5;9mcurly\x1b[24m")
		case 5:
			if osc && i%5000 == 5 {
				b.WriteString("\x1b]0;title with\nnewline\x07") // hidden, so not a line start or cut point
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// TestReflowMatchesSequentialIndex checks that a parallel reflow, merged under
// a budget, ends with the same rows, styles, and lines as a fresh index.
func TestReflowMatchesSequentialIndex(t *testing.T) {
	if reflowWorkers() < 2 {
		t.Skip("reflow needs two workers")
	}
	for _, osc := range []bool{false, true} {
		t.Run(fmt.Sprintf("osc=%v", osc), func(t *testing.T) {
			source := benchmarkSource{strings.NewReader(reflowInput(osc))}
			text := NewText(source)
			defer text.Close()
			if err := text.Update(80, true); err != nil {
				t.Fatal(err)
			}
			if err := text.UpdateFor(37, true, time.Millisecond); err != nil {
				t.Fatal(err)
			}
			if text.reflow == nil && text.NeedsUpdate(37, true) {
				t.Fatal("width change did not start a reflow")
			}
			for steps := 0; text.NeedsUpdate(37, true); steps++ {
				if steps > 100000 {
					t.Fatal("reflow stopped progressing")
				}
				if err := text.UpdateFor(37, true, time.Millisecond); err != nil {
					t.Fatal(err)
				}
			}
			fresh := NewText(source)
			defer fresh.Close()
			if err := fresh.Update(37, true); err != nil {
				t.Fatal(err)
			}
			compareIndexes(t, text, fresh)
		})
	}
}

func compareIndexes(t *testing.T, got, want *Text) {
	t.Helper()
	if got.RowCount() != want.RowCount() || got.LineCount() != want.LineCount() {
		t.Fatalf("rows/lines %d/%d, want %d/%d", got.RowCount(), got.LineCount(), want.RowCount(), want.LineCount())
	}
	for row := 0; row < want.starts.count; row++ {
		if got.starts.at(row) != want.starts.at(row) {
			t.Fatalf("row %d starts at %d, want %d", row, got.starts.at(row), want.starts.at(row))
		}
		gotStyle, gotUnderline := got.styleOfRow(row)
		wantStyle, wantUnderline := want.styleOfRow(row)
		if gotStyle != wantStyle || gotUnderline != wantUnderline {
			t.Fatalf("row %d style %v/%v, want %v/%v", row, gotStyle, gotUnderline, wantStyle, wantUnderline)
		}
	}
	for line := 0; line < want.lineStarts.count; line++ {
		if got.lineStarts.at(line) != want.lineStarts.at(line) {
			t.Fatalf("line %d starts at %d, want %d", line, got.lineStarts.at(line), want.lineStarts.at(line))
		}
	}
	for _, top := range []int{0, want.RowCount() / 3, want.RowCount() - 24} {
		gotLines, err := got.Lines(top, 24)
		if err != nil {
			t.Fatal(err)
		}
		wantLines, _ := want.Lines(top, 24)
		if strings.Join(gotLines, "\n") != strings.Join(wantLines, "\n") {
			t.Fatalf("lines at row %d differ", top)
		}
	}
}
