package filter

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/araaha/glimpse/internal/search"
)

// growing is a document that input is appended to.
type growing struct {
	mu   sync.Mutex
	data []byte
}

func (g *growing) append(text string) { g.mu.Lock(); g.data = append(g.data, text...); g.mu.Unlock() }
func (g *growing) Len() int64         { g.mu.Lock(); defer g.mu.Unlock(); return int64(len(g.data)) }
func (g *growing) ReadAt(p []byte, off int64) (int, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if off >= int64(len(g.data)) {
		return 0, io.EOF
	}
	n := copy(p, g.data[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func contents(t *testing.T, s *Source) string {
	t.Helper()
	data := make([]byte, s.Len())
	if _, err := s.ReadAt(data, 0); err != nil && err != io.EOF {
		t.Fatal(err)
	}
	return string(data)
}

func scan(t *testing.T, s *Source, complete bool) {
	t.Helper()
	s.complete.Store(complete)
	if err := s.update(context.Background()); err != nil {
		t.Fatal(err)
	}
}

var options = search.Options{ChunkSize: 8, Workers: 2}

func TestSourceContents(t *testing.T) {
	document := &growing{}
	document.append("a1\nb\na2\na3\nc\na4")
	s := New(document, search.Query{Text: "a"}, options, 1<<20)
	scan(t, s, false)
	if got, want := contents(t, s), reset+"a1\n"+reset+"a2\na3\n"; got != want {
		t.Fatalf("contents %q, want %q", got, want)
	}
	if s.Final() {
		t.Fatal("final before the input ended")
	}
	scan(t, s, true)
	if got, want := contents(t, s), reset+"a1\n"+reset+"a2\na3\n"+reset+"a4"; got != want {
		t.Fatalf("contents %q, want %q", got, want)
	}
	if !s.Final() || s.Count() != 4 || s.LastLine() != 6 {
		t.Fatalf("final %v, count %d, last line %d", s.Final(), s.Count(), s.LastLine())
	}
	for vline, line := range map[int64]int64{1: 1, 2: 3, 3: 4, 4: 6} {
		if got := s.RealLine(vline); got != line {
			t.Errorf("RealLine(%d) = %d, want %d", vline, got, line)
		}
	}
	for line, vline := range map[int64]int64{1: 1, 2: 2, 3: 2, 5: 4, 6: 4, 7: 5} {
		if got, ok := s.VirtualLine(line); got != vline || !ok {
			t.Errorf("VirtualLine(%d) = %d, %v, want %d", line, got, ok, vline)
		}
	}
	// "b\n" is hidden: offsets in it map to the next matching line, "a2".
	a2 := int64(strings.Index(contents(t, s), "a2"))
	if got, _ := s.VirtualOffset(3); got != a2-int64(len(reset)) {
		t.Errorf("VirtualOffset(3) = %d, want %d", got, a2-int64(len(reset)))
	}
	if got, _ := s.VirtualOffset(6); got != a2+1 {
		t.Errorf("VirtualOffset(6) = %d, want %d", got, a2+1)
	}
	if got := s.RealOffset(a2 + 1); got != 6 {
		t.Errorf("RealOffset(%d) = %d, want 6", a2+1, got)
	}
}

// A final line without a newline grows as more input arrives, without
// running into the next line shown.
func TestSourceOpenLine(t *testing.T) {
	document := &growing{}
	document.append("x\na")
	s := New(document, search.Query{Text: "a"}, options, 1<<20)
	scan(t, s, true)
	if got := contents(t, s); got != reset+"a" {
		t.Fatalf("contents %q", got)
	}
	document.append("bc\nd\nab\n")
	scan(t, s, true)
	if got, want := contents(t, s), reset+"abc\n"+reset+"ab\n"; got != want {
		t.Fatalf("contents %q, want %q", got, want)
	}
	if got := s.RealLine(2); got != 4 {
		t.Fatalf("RealLine(2) = %d, want 4", got)
	}
}

func TestSourceUndecided(t *testing.T) {
	document := &growing{}
	document.append("a\nb\nlong")
	s := New(document, search.Query{Text: "zzz"}, options, 1<<20)
	scan(t, s, false)
	if _, ok := s.VirtualOffset(6); ok {
		t.Fatal("an unfinished line was decided")
	}
	if _, ok := s.VirtualLine(3); ok {
		t.Fatal("an unfinished line was decided")
	}
	if got, ok := s.VirtualOffset(1); got != 0 || !ok {
		t.Fatalf("VirtualOffset(1) = %d, %v", got, ok)
	}
}

func TestSourceQuota(t *testing.T) {
	document := &growing{}
	document.append("a\nb\na\nb\na\n")
	s := New(document, search.Query{Text: "a"}, options, 2*segmentBytes)
	s.complete.Store(true)
	if err := s.update(context.Background()); err == nil {
		t.Fatal("the quota was not enforced")
	}
	if got, want := contents(t, s), reset+"a\n"+reset+"a\n"; got != want {
		t.Fatalf("contents %q, want %q", got, want)
	}
}
