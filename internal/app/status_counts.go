package app

import (
	"context"
	"runtime"
	"sort"
	"sync"
	"time"

	"github.com/araaha/glimpse/internal/search"
)

type countKey struct {
	query      search.Query
	base, end  int64
	generation int64 // document content generation; reloads invalidate counts
}

// Dense chunks only resolve the selected rank instead of retaining offsets.
const defaultMaxChunkOffsets = 1 << 16

// countResult is an immutable per-chunk match index for a snapshot, plus the
// match offsets of the chunk holding the last requested target.
type countResult struct {
	key        countKey
	generation uint64
	chunks     search.Chunks
	total      int64
	target     int64
	rankChunk  int     // Chunk whose matches below resolve ranks, or -1.
	offsets    []int64 // All match starts in rankChunk, when not too dense.
	within     int64   // Rank of target within rankChunk; 0 if not a match.
	rankKnown  bool
	err        error
}

// rank returns the one-based ordinal of a match start, and whether it is known.
// Offsets that are not matches rank 0.
func (r *countResult) rank(offset int64) (int64, bool) {
	c := r.chunks.Find(offset)
	if c < 0 {
		return 0, true
	}
	if c != r.rankChunk {
		return 0, false
	}
	if r.offsets != nil {
		i := sort.Search(len(r.offsets), func(i int) bool { return r.offsets[i] >= offset })
		if i == len(r.offsets) || r.offsets[i] != offset {
			return 0, true
		}
		return r.chunks.Before(c) + int64(i) + 1, true
	}
	if !r.rankKnown || offset != r.target {
		return 0, false
	}
	if r.within == 0 {
		return 0, true
	}
	return r.chunks.Before(c) + r.within, true
}

type countSnapshot struct {
	search.Source
	key countKey
}

func (s countSnapshot) Len() int64   { return s.key.end }
func (s countSnapshot) Start() int64 { return s.key.base }

// All state except the immutable task/result belongs to the UI event loop.
type statusCounts struct {
	results    chan countResult
	workers    sync.WaitGroup
	cancel     context.CancelFunc
	generation uint64
	running    bool
	visible    bool // The statusline currently shows the search counter.
	task       countKey
	last       *countResult
	next       time.Time
	chunkSize  int64 // search.DefaultChunkSize when zero.
	maxOffsets int64
}

func newStatusCounts() *statusCounts {
	return &statusCounts{results: make(chan countResult, 1), maxOffsets: defaultMaxChunkOffsets}
}

func (s *statusCounts) stop() {
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.generation++
	s.running = false
}

func (s *statusCounts) close() { s.stop(); s.workers.Wait() }

func sameCountQuery(a, b countKey) bool {
	return a.query == b.query && a.generation == b.generation
}

// chunksFor returns the exact chunk index for a snapshot, if one is ready, so
// searches can skip chunks without matches.
func (s *statusCounts) chunksFor(key countKey) (search.Chunks, bool) {
	if s.last == nil || s.last.key != key || s.last.err != nil || key.query.Text == "" {
		return search.Chunks{}, false
	}
	return s.last.chunks, true
}

func (s *statusCounts) request(ctx context.Context, source search.Source, key countKey, target int64, matched, enabled bool) {
	if !enabled || key.query.Text == "" {
		if s.running {
			s.stop()
		}
		return
	}
	if s.running {
		if sameCountQuery(s.task, key) && s.task.base == key.base {
			return
		} // Let an append-only snapshot finish.
		s.stop()
	}
	var previous search.Chunks
	if s.last != nil && s.last.key == key {
		if s.last.err != nil || !matched {
			return
		}
		if _, known := s.last.rank(target); known {
			return
		}
		previous = s.last.chunks // Only the target's chunk needs scanning.
	} else if s.last != nil && sameCountQuery(s.last.key, key) && s.last.err == nil {
		// Limit recounts of a continuously growing stream. Each one only
		// rescans the open tail and the selected match's chunk.
		if time.Now().Before(s.next) {
			return
		}
		previous = s.last.chunks
	}
	ctx, s.cancel = context.WithCancel(ctx)
	s.running, s.task = true, key
	generation, chunkSize, maxOffsets := s.generation, s.chunkSize, s.maxOffsets
	s.workers.Add(1)
	go func() {
		defer s.workers.Done()
		result := countResult{key: key, generation: generation, target: target, rankChunk: -1}
		source := countSnapshot{Source: source, key: key}
		options := search.Options{Query: key.query, ChunkSize: chunkSize, Workers: max(1, runtime.GOMAXPROCS(0)-1)}
		result.chunks, result.err = search.UpdateChunks(ctx, source, previous, key.base, key.end, options)
		result.total = result.chunks.Total()
		if result.err == nil && matched {
			result.err = result.resolveRank(ctx, source, maxOffsets)
		}
		select {
		case s.results <- result:
		case <-ctx.Done():
		}
	}()
}

// resolveRank scans only the chunk containing the target.
func (r *countResult) resolveRank(ctx context.Context, source search.Source, maxOffsets int64) error {
	c := r.chunks.Find(r.target)
	if c < 0 {
		return nil
	}
	chunk := r.chunks.List[c]
	r.rankChunk = c
	if chunk.Count <= maxOffsets {
		r.offsets = make([]int64, 0, chunk.Count)
		return search.WalkRange(ctx, source, r.key.query, chunk.Start, chunk.End-1, r.key.end, func(offset, _ int64) bool {
			r.offsets = append(r.offsets, offset)
			return true
		})
	}
	var seen int64
	err := search.WalkRange(ctx, source, r.key.query, chunk.Start, r.target, r.key.end, func(offset, _ int64) bool {
		seen++
		if offset == r.target {
			r.within = seen
		}
		return true
	})
	r.rankKnown = err == nil
	return err
}

// update requests counts for the visible counter. Match navigation takes
// priority over a new scan: a compatible count may finish, but obsolete work
// is stopped.
func (s *statusCounts) update(ctx context.Context, source search.Source, key countKey, target int64, matched, navigating bool) {
	if !navigating {
		s.request(ctx, source, key, target, matched, s.visible)
	} else if s.running && (!s.visible || !sameCountQuery(s.task, key) || s.task.base != key.base) {
		s.stop()
	}
}

// recountDue reports whether a throttled recount of grown input may start.
func (s *statusCounts) recountDue(now time.Time, end int64) bool {
	return s.visible && !s.running && s.last != nil && s.last.key.end != end && !now.Before(s.next)
}

func (s *statusCounts) accept(result countResult) bool {
	if result.generation != s.generation {
		return false
	}
	s.running = false
	s.last = &result
	s.next = time.Now().Add(200 * time.Millisecond)
	return true
}

func (s *statusCounts) values(key countKey, target int64, matched bool) (current, total int64, currentKnown, totalKnown bool) {
	if s.last == nil || s.last.key != key || s.last.err != nil {
		return 0, 0, !matched, false
	}
	if !matched {
		return 0, s.last.total, true, true
	}
	current, currentKnown = s.last.rank(target)
	return current, s.last.total, currentKnown, true
}
