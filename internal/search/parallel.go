package search

import (
	"context"
	"errors"
	"sync"
)

// errStraddled reports a candidate chunk boundary that a token spans, so the
// following chunk cannot be decoded on its own.
var errStraddled = errors.New("token straddles a chunk boundary")

// FindParallel has FindFrom's semantics but decodes chunks on several workers.
// Forward, the first chunk holding a match settles the result and cancels the
// rest, so a nearby match costs about one chunk while a distant or absent one
// is found at the parallel scan rate. Backward, as in FindFrom, the whole
// prefix is scanned.
func FindParallel(ctx context.Context, source Source, query Query, origin, anchor int64, backward, include bool, options Options) (Result, error) {
	if options.Workers <= 1 {
		return FindFrom(ctx, source, query, origin, anchor, backward, include)
	}
	base, end := sourceStart(source), source.Len()
	origin = min(max(base, origin), end)
	anchor = min(max(base, anchor), origin)
	eligible := func(offset int64) bool {
		if backward {
			return offset < origin || include && offset == origin
		}
		return offset > origin || include && offset == origin
	}
	every := func(int64) bool { return true }
	scan := orderedScan{ctx: ctx, source: source, query: query, end: end, options: options}
	through := min(origin+1, end) // ranges below end at origin, inclusive
	var found Result
	var err error
	if !backward {
		if found, err = scan.find(anchor, end, eligible, false); found.Found || err != nil {
			return scan.settle(found, err, origin, anchor, backward, include)
		}
		// Every match lies at or before origin; wrap to the first one.
		found, err = scan.find(base, through, every, false)
	} else {
		if found, err = scan.find(base, through, eligible, true); found.Found || err != nil {
			return scan.settle(found, err, origin, anchor, backward, include)
		}
		// Every match lies at or after origin; wrap to the last one.
		found, err = scan.find(anchor, end, every, true)
	}
	found.Wrapped = found.Found
	return scan.settle(found, err, origin, anchor, backward, include)
}

type orderedScan struct {
	ctx     context.Context
	source  Source
	query   Query
	end     int64 // decoding limit: the snapshot end
	options Options
}

// settle falls back to a sequential search when chunks could not be decoded
// independently.
func (s orderedScan) settle(found Result, err error, origin, anchor int64, backward, include bool) (Result, error) {
	if errors.Is(err, errStraddled) {
		return FindFrom(s.ctx, s.source, s.query, origin, anchor, backward, include)
	}
	return found, err
}

type chunkResult struct {
	found     Result
	straddled bool
	err       error
	done      chan struct{}
}

// find returns the first (or, with last, the final) match starting in
// [start, stop) that want accepts. start must be a restart point.
func (s orderedScan) find(start, stop int64, want func(int64) bool, last bool) (Result, error) {
	if start >= stop {
		return Result{}, nil
	}
	size := s.options.ChunkSize
	if size <= 0 {
		size = DefaultChunkSize
	}
	bounds := []int64{start}
	for next := start + size; next < stop; {
		boundary, err := lineStart(s.source, next, stop)
		if err != nil {
			return Result{}, err
		}
		if boundary < 0 {
			break
		}
		bounds = append(bounds, boundary)
		next = boundary + size
	}
	bounds = append(bounds, stop)
	count := len(bounds) - 1
	ctx, cancel := context.WithCancel(s.ctx)
	results := make([]chunkResult, count)
	for i := range results {
		results[i].done = make(chan struct{})
	}
	var (
		wait sync.WaitGroup
		mu   sync.Mutex
		next int
	)
	defer func() { cancel(); wait.Wait() }()
	for range min(s.options.Workers, count) {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for {
				mu.Lock()
				i := next
				next++
				mu.Unlock()
				if i >= count || ctx.Err() != nil {
					return
				}
				result := &results[i]
				boundary := int64(0)
				if bounds[i+1] < s.end {
					boundary = bounds[i+1]
				}
				result.straddled, _, result.err = walkChecked(ctx, s.source, s.query, bounds[i], s.end, bounds[i+1]-1, boundary, func(offset, _ int64) bool {
					if want(offset) {
						result.found = Result{Offset: offset, Found: true}
						return last
					}
					return true
				})
				close(result.done)
			}
		}()
	}
	settled := func(i int) *chunkResult {
		select {
		case <-results[i].done:
		case <-ctx.Done():
			return &chunkResult{err: ctx.Err()}
		}
		return &results[i]
	}
	// A chunk's own scan is exact only if every earlier chunk ended at a token
	// boundary; otherwise it was decoded from a point inside a token.
	var found Result
	for i := range count {
		result := settled(i)
		if result.err != nil {
			return Result{}, result.err
		}
		if result.found.Found {
			if !last {
				return result.found, nil
			}
			found = result.found
		}
		if result.straddled && i < count-1 {
			return Result{}, errStraddled
		}
	}
	return found, nil
}
