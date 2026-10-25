package ui

import (
	"glimpse/internal/pager"
	"os"
	"strings"
	"testing"
)

func TestBoundedIndexPagesAndCleanup(t *testing.T) {
	doc := pager.NewDocument()
	defer doc.Close()
	doc.Write([]byte(strings.Repeat("abc\n", 3000)))
	text, err := NewTextWithLimits(doc, 16<<10, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer text.Close()
	if err := text.Update(2, true); err != nil {
		t.Fatal(err)
	}
	if text.starts.file == nil || text.lineStarts.file == nil {
		t.Fatal("indexes did not spill")
	}
	for _, line := range []int{1, 512, 513, 1024, 2999, 3000} {
		if row := text.LineRow(line); row != (line-1)*2 {
			t.Fatalf("line %d row %d", line, row)
		}
	}
	for _, row := range []int{0, 400, 1024, 5999} {
		if got := text.RowAt(text.Offset(row)); got != row {
			t.Fatalf("row round trip %d -> %d", row, got)
		}
	}
	name := text.starts.file.Name()
	if err := text.Update(10, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(name); !os.IsNotExist(err) {
		t.Fatalf("resize leaked index file: %v", err)
	}
	name = text.lineStarts.file.Name()
	text.Close()
	if _, err := os.Stat(name); !os.IsNotExist(err) {
		t.Fatalf("close leaked index file: %v", err)
	}
}

func TestMetadataQuotaIsExplicit(t *testing.T) {
	doc := pager.NewDocument()
	defer doc.Close()
	doc.Write([]byte("a\nb\nc\n"))
	text, _ := NewTextWithLimits(doc, 16<<10, 32)
	defer text.Close()
	if err := text.Update(10, true); err == nil || !strings.Contains(err.Error(), "metadata quota") {
		t.Fatalf("metadata limit = %v", err)
	}
}

func TestRollingTextRebasesAfterEviction(t *testing.T) {
	doc, _ := pager.NewDocumentWithOptions(16, pager.StorageOptions{MemoryBudget: 64, SpoolQuota: 128, Retention: "rolling"})
	defer doc.Close()
	text := NewText(doc)
	defer text.Close()
	doc.Write([]byte(strings.Repeat("old\n", 8)))
	if err := text.Update(10, false); err != nil {
		t.Fatal(err)
	}
	doc.Write([]byte(strings.Repeat("new\n", 20)))
	if err := text.Update(10, true); err != nil {
		t.Fatal(err)
	}
	lines, err := text.Lines(0, 100)
	if err != nil || len(lines) != 16 {
		t.Fatalf("rolling lines = %q, %v", lines, err)
	}
	for _, line := range lines {
		if line != "new" {
			t.Fatalf("retained evicted content: %q", line)
		}
	}
}
