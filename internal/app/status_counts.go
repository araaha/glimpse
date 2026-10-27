package app

import (
	"context"
	"sync"
	"time"

	"glimpse/internal/search"
)

type countKey struct {
	query         string
	smart, ignore bool
	base, end     int64
}

type rankedMatch struct{ offset, rank int64 }

// A bounded neighborhood, plus the ends, makes n/N and wraparound inexpensive
// without retaining an offset (or Go object) for every match in a huge file.
type countResult struct {
	key           countKey
	generation    uint64
	total         int64
	target        int64
	first, last   [32]rankedMatch
	before, after [64]rankedMatch
	err           error
}

func (r *countResult) rank(offset int64) (int64, bool) {
	for _, matches := range [][]rankedMatch{r.first[:], r.last[:], r.before[:], r.after[:]} {
		for _, match := range matches {
			if match.rank > 0 && match.offset == offset {
				return match.rank, true
			}
		}
	}
	return 0, false
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
}

func newStatusCounts() *statusCounts { return &statusCounts{results: make(chan countResult, 1)} }

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
	return a.query == b.query && a.smart == b.smart && a.ignore == b.ignore && a.base == b.base
}

func (s *statusCounts) request(ctx context.Context, source search.Source, key countKey, target int64, matched, enabled bool) {
	if !enabled || key.query == "" {
		if s.running {
			s.stop()
		}
		return
	}
	if s.running {
		if sameCountQuery(s.task, key) {
			return
		} // Let an append-only snapshot finish.
		s.stop()
	}
	if s.last != nil && s.last.key == key {
		_, cached := s.last.rank(target)
		if !matched || cached || s.last.target == target || s.last.total == 0 || s.last.err != nil {
			return
		}
	}
	// Limit repeated whole-snapshot counts on a continuously growing stream.
	if s.last != nil && sameCountQuery(s.last.key, key) && s.last.key != key && time.Now().Before(s.next) {
		return
	}
	s.stop()
	ctx, s.cancel = context.WithCancel(ctx)
	s.running, s.task = true, key
	generation := s.generation
	s.workers.Add(1)
	go func() {
		defer s.workers.Done()
		result := countResult{key: key, generation: generation, target: target}
		afterCount := 0
		result.err = search.Walk(ctx, countSnapshot{Source: source, key: key}, key.query, key.base, key.end, key.smart, key.ignore, func(offset, end int64) bool {
			result.total++
			match := rankedMatch{offset, result.total}
			if result.total <= int64(len(result.first)) {
				result.first[result.total-1] = match
			}
			result.last[result.total%int64(len(result.last))] = match
			if offset <= target {
				result.before[result.total%int64(len(result.before))] = match
			}
			if offset >= target && afterCount < len(result.after) {
				result.after[afterCount] = match
				afterCount++
			}
			return true
		})
		select {
		case s.results <- result:
		case <-ctx.Done():
		}
	}()
}

// update requests totals for the visible counter. Match navigation takes
// priority over a new whole-file scan: a compatible count may finish, but
// obsolete work is stopped.
func (s *statusCounts) update(ctx context.Context, source search.Source, key countKey, target int64, matched, navigating bool) {
	if !navigating {
		s.request(ctx, source, key, target, matched, s.visible)
	} else if s.running && (!s.visible || !sameCountQuery(s.task, key)) {
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
	current, currentKnown = s.last.rank(target)
	if !matched || !currentKnown && (s.last.target == target || s.last.total == 0) {
		current, currentKnown = 0, true
	}
	return current, s.last.total, currentKnown, true
}
