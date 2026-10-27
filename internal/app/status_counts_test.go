package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"glimpse/internal/pager"
	"glimpse/internal/search"
)

type countTestSource struct{ *strings.Reader }

func (s countTestSource) Len() int64 { return s.Size() }

func awaitCounts(t *testing.T, counts *statusCounts) *countResult {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case response := <-counts.results:
			if counts.accept(response) {
				return counts.last
			}
		case <-deadline.C:
			t.Fatal("timed out waiting for status counts")
		}
	}
}

func TestStatusCountsCacheAndOrdinal(t *testing.T) {
	source := countTestSource{strings.NewReader(strings.Repeat("hit\n", 1000))}
	key := countKey{query: "hit", end: source.Len()}
	counts := newStatusCounts()
	defer counts.close()
	counts.request(context.Background(), source, key, 4*499, true, true)
	result := awaitCounts(t, counts)
	if result.err != nil || result.total != 1000 {
		t.Fatalf("counts = %+v", result)
	}
	for _, index := range []int64{0, 499, 500, 530, 999} {
		current, total, currentKnown, totalKnown := counts.values(key, 4*index, true)
		if !currentKnown || !totalKnown || current != index+1 || total != 1000 {
			t.Fatalf("rank %d = %d/%d, known %v/%v", index, current, total, currentKnown, totalKnown)
		}
		generation := counts.generation
		counts.request(context.Background(), source, key, 4*index, true, true)
		if counts.running || counts.generation != generation {
			t.Fatal("nearby repeat rescanned the file")
		}
	}
	// The whole input is one chunk with cached offsets: distant ordinals need no scan.
	counts.request(context.Background(), source, key, 4*800, true, true)
	if counts.running {
		t.Fatal("distant ordinal in a cached chunk rescanned")
	}
	if current, _, known, _ := counts.values(key, 4*800, true); !known || current != 801 {
		t.Fatalf("distant ordinal = %d, known %v", current, known)
	}
	// A stale selected match under changed case options must not cause a recount loop.
	counts.request(context.Background(), source, key, 4*800+1, true, true)
	if counts.running {
		t.Fatal("non-matching selected offset triggered an endless recount")
	}
	if current, _, known, _ := counts.values(key, 4*800+1, true); current != 0 || !known {
		t.Fatal("invalid selected match did not show zero")
	}
}

func TestStatusCountsVisibleSearchSemantics(t *testing.T) {
	input := "\x1b[31mh\x1b[mi\bt\bt\n_\bhit\n\x1b]52;hit\nhit\ahit\nHIT"
	source := countTestSource{strings.NewReader(input)}
	counts := newStatusCounts()
	defer counts.close()
	key := countKey{query: "hit", ignore: true, end: source.Len()}
	target := int64(strings.LastIndex(input, "HIT"))
	var wantTotal, wantCurrent int64
	search.Walk(context.Background(), source, "hit", 0, source.Len(), false, true, func(offset, end int64) bool {
		wantTotal++
		if offset == target {
			wantCurrent = wantTotal
		}
		return true
	})
	counts.request(context.Background(), source, key, target, true, true)
	awaitCounts(t, counts)
	current, total, ck, tk := counts.values(key, target, true)
	if !ck || !tk || current != wantCurrent || total != wantTotal || total != 3 {
		t.Fatalf("visible counts = %d/%d, want %d/%d", current, total, wantCurrent, wantTotal)
	}
}

type gatedCountSource struct {
	countTestSource
	started chan struct{}
	release <-chan struct{}
}

func (s gatedCountSource) ReadAt(p []byte, offset int64) (int, error) {
	select {
	case s.started <- struct{}{}:
	default:
	}
	<-s.release
	return s.countTestSource.ReadAt(p, offset)
}

func TestStatusCountsCancellationAndStaleResults(t *testing.T) {
	gate := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(gate) }) }
	t.Cleanup(release)
	source := gatedCountSource{countTestSource{strings.NewReader("first\nsecond\nsecond")}, make(chan struct{}, 2), gate}
	counts := newStatusCounts()
	defer func() { release(); counts.close() }()
	first := countKey{query: "first", end: source.Len()}
	counts.request(context.Background(), source, first, 0, false, false)
	if counts.running {
		t.Fatal("disabled section started a scan")
	}
	counts.request(context.Background(), source, first, 0, true, true)
	select {
	case <-source.started:
	case <-time.After(time.Second):
		t.Fatal("first worker never started")
	}
	old := counts.generation
	counts.request(context.Background(), source, first, 0, true, false)
	if counts.running {
		t.Fatal("hiding the statusline did not cancel a running count")
	}
	second := first
	second.query = "second"
	counts.request(context.Background(), source, second, 6, true, true)
	select {
	case <-source.started:
	case <-time.After(time.Second):
		t.Fatal("replacement worker never started")
	}
	if counts.accept(countResult{key: first, generation: old, total: 99}) {
		t.Fatal("accepted stale query counts")
	}
	release()
	result := awaitCounts(t, counts)
	if result.key.query != "second" || result.total != 2 || result.err != nil {
		t.Fatalf("replacement counts = %+v", result)
	}
	counts.request(context.Background(), source, second, 6, true, false)
	if counts.running {
		t.Fatal("hidden section kept worker active")
	}
}

func TestStatusCountsAppendAndRollingInvalidation(t *testing.T) {
	doc := pager.NewDocument()
	defer doc.Close()
	doc.Write([]byte("hit\nhit\n"))
	counts := newStatusCounts()
	defer counts.close()
	key := countKey{query: "hit", end: doc.Len()}
	counts.request(context.Background(), doc, key, 0, true, true)
	awaitCounts(t, counts)
	doc.Write([]byte("hit\n"))
	key.end = doc.Len()
	if _, _, _, known := counts.values(key, 0, true); known {
		t.Fatal("old total presented as exact after append")
	}
	counts.request(context.Background(), doc, key, 0, true, true)
	if counts.running {
		t.Fatal("live recount was not throttled")
	}
	counts.next = time.Time{}
	counts.request(context.Background(), doc, key, 0, true, true)
	if result := awaitCounts(t, counts); result.total != 3 {
		t.Fatalf("appended total = %d", result.total)
	}
	options := pager.DefaultStorageOptions()
	options.Retention, options.MemoryBudget = "rolling", 64
	ring, err := pager.NewDocumentWithOptions(16, options)
	if err != nil {
		t.Fatal(err)
	}
	defer ring.Close()
	ring.Write([]byte(strings.Repeat("hit\n", 64)))
	key.base, key.end = ring.Start(), ring.Len()
	counts.next = time.Time{} // A moved base is throttled like growth.
	counts.request(context.Background(), ring, key, ring.Start(), true, true)
	result := awaitCounts(t, counts)
	if result.err != nil || result.total != 16 {
		t.Fatalf("retained total = %d, %v", result.total, result.err)
	}
	if current, _, ck, tk := counts.values(key, ring.Start(), true); !ck || !tk || current != 1 {
		t.Fatal("rolling match rank was not rebased")
	}
}

type failedCountSource struct{}

func (failedCountSource) Len() int64 { return 10 }
func (failedCountSource) ReadAt([]byte, int64) (int, error) {
	return 0, errors.New("failed status read")
}

func TestStatusCountsFailureDoesNotLoop(t *testing.T) {
	counts := newStatusCounts()
	defer counts.close()
	key := countKey{query: "hit", end: 10}
	counts.request(context.Background(), failedCountSource{}, key, 0, true, true)
	if result := awaitCounts(t, counts); result.err == nil {
		t.Fatal("read error lost")
	}
	counts.request(context.Background(), failedCountSource{}, key, 1, true, true)
	if counts.running {
		t.Fatal("failed snapshot repeatedly rescanned")
	}
	if _, _, _, known := counts.values(key, 0, true); known {
		t.Fatal("failed count published as exact")
	}
}

// Ranks combine earlier chunks' counts with one scan of the target's chunk;
// dense chunks resolve only the selected rank.
func TestStatusCountsRanksAcrossChunks(t *testing.T) {
	source := countTestSource{strings.NewReader(strings.Repeat("hit hit\n", 500))}
	key := countKey{query: "hit", end: source.Len()}
	for _, maxOffsets := range []int64{defaultMaxChunkOffsets, 2} {
		counts := newStatusCounts()
		counts.chunkSize, counts.maxOffsets = 64, maxOffsets
		target := func(index int64) int64 { return 8*(index/2) + 4*(index%2) }
		counts.request(context.Background(), source, key, target(0), true, true)
		result := awaitCounts(t, counts)
		if result.err != nil || result.total != 1000 || len(result.chunks.List) < 10 {
			t.Fatalf("chunked counts = total %d, %d chunks, %v", result.total, len(result.chunks.List), result.err)
		}
		for _, index := range []int64{0, 1, 157, 500, 999} {
			counts.request(context.Background(), source, key, target(index), true, true)
			if counts.running {
				awaitCounts(t, counts)
			}
			current, total, known, totalKnown := counts.values(key, target(index), true)
			if !known || !totalKnown || current != index+1 || total != 1000 {
				t.Fatalf("max offsets %d: rank of %d = %d/%d known %v", maxOffsets, index, current, total, known)
			}
			if chunks, ok := counts.chunksFor(key); !ok || chunks.Total() != 1000 {
				t.Fatal("exact chunk index unavailable for search")
			}
		}
		// A selected offset that is not a match ranks zero, without rescanning.
		counts.request(context.Background(), source, key, target(10)+1, true, true)
		if counts.running {
			awaitCounts(t, counts)
		}
		counts.request(context.Background(), source, key, target(10)+1, true, true)
		if counts.running {
			t.Fatal("non-match repeatedly rescanned")
		}
		if current, _, known, _ := counts.values(key, target(10)+1, true); current != 0 || !known {
			t.Fatalf("non-match rank = %d, known %v", current, known)
		}
		stale := key
		stale.end--
		if _, ok := counts.chunksFor(stale); ok {
			t.Fatal("chunk index offered for a different snapshot")
		}
		counts.close()
	}
}

// Appends reuse settled chunks: only the open tail is rescanned.
func TestStatusCountsAppendRescansTail(t *testing.T) {
	doc := pager.NewDocument()
	defer doc.Close()
	doc.Write([]byte(strings.Repeat("hit\n", 1000)))
	counts := newStatusCounts()
	defer counts.close()
	counts.chunkSize = 256
	key := countKey{query: "hit", end: doc.Len()}
	counts.request(context.Background(), doc, key, 0, false, true)
	before := awaitCounts(t, counts).chunks.List
	doc.Write([]byte("hi"))
	doc.Write([]byte("t\n"))
	key.end = doc.Len()
	counts.next = time.Time{}
	counts.request(context.Background(), doc, key, 0, false, true)
	after := awaitCounts(t, counts)
	if after.total != 1001 {
		t.Fatalf("appended total = %d", after.total)
	}
	for i := range before[:len(before)-1] {
		if after.chunks.List[i] != before[i] {
			t.Fatalf("settled chunk %d changed: %+v -> %+v", i, before[i], after.chunks.List[i])
		}
	}
}
