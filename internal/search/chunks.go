package search

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sort"
	"sync"
)

// DefaultChunkSize keeps one chunk scan to roughly ten milliseconds on
// escape-heavy input, while a gigabyte needs only about a thousand chunks.
const DefaultChunkSize = 1 << 20

// Chunk counts the matches whose first byte lies in [Start, End). Every chunk
// starts at a verified restart point: decoding from Start yields the same
// tokens as decoding straight through from the previous chunk.
type Chunk struct {
	Start, End, Count int64
	// open means its scan reached the snapshot end, so Count may change when
	// input grows (a later byte can complete a match that starts here).
	open bool
}

// Chunks partitions a snapshot [Start, End) of retained input for one query.
// The final chunk ends at the snapshot end, which is not a restart point.
type Chunks struct {
	List   []Chunk
	prefix []int64 // prefix[i] is the number of matches before List[i].
}

func newChunks(list []Chunk) Chunks {
	prefix := make([]int64, len(list)+1)
	for i, chunk := range list {
		prefix[i+1] = prefix[i] + chunk.Count
	}
	return Chunks{List: list, prefix: prefix}
}

func (c Chunks) Start() int64 {
	if len(c.List) == 0 {
		return 0
	}
	return c.List[0].Start
}

func (c Chunks) End() int64 {
	if len(c.List) == 0 {
		return 0
	}
	return c.List[len(c.List)-1].End
}

func (c Chunks) Total() int64 {
	if len(c.prefix) == 0 {
		return 0
	}
	return c.prefix[len(c.prefix)-1]
}

// Before returns the number of matches in chunks preceding chunk i.
func (c Chunks) Before(i int) int64 { return c.prefix[i] }

// Find returns the chunk containing offset, or -1.
func (c Chunks) Find(offset int64) int {
	i := sort.Search(len(c.List), func(i int) bool { return c.List[i].End > offset })
	if i == len(c.List) || offset < c.List[i].Start {
		return -1
	}
	return i
}

// Options describe one query and how its chunks are scanned.
type Options struct {
	Query     Query
	ChunkSize int64 // DefaultChunkSize when zero.
	Workers   int   // One when zero.
}

// WalkRange visits matches starting in [start, through], decoding no further
// than limit. start must be a restart point.
func WalkRange(ctx context.Context, source Source, query Query, start, through, limit int64, visit func(start, end int64) bool) error {
	return walk(ctx, source, query, start, limit, through, visit)
}

// UpdateChunks returns chunks covering [base, end) of source, reusing previous
// chunks for the same query where their bytes are still retained. Appending only
// rescans the old final chunk and new input; a moved base only rescans the head.
func UpdateChunks(ctx context.Context, source Source, previous Chunks, base, end int64, options Options) (Chunks, error) {
	kept := previous.List
	if previous.End() > end {
		kept = nil // Not a later snapshot of the same input.
	}
	for len(kept) > 0 && kept[0].Start < base {
		kept = kept[1:]
	}
	// Growth can complete a match that started in an open chunk, or (for the
	// final chunk) add matches; rescan from the first open chunk.
	if len(kept) > 0 && previous.End() != end {
		for i, chunk := range kept {
			if chunk.open {
				kept = kept[:i]
				break
			}
		}
	}
	for len(kept) > 0 && kept[0].Start > base {
		head, ok, err := countChunks(ctx, source, base, kept[0].Start, end, options)
		if err != nil {
			return Chunks{}, err
		}
		if ok {
			kept = append(head, kept...)
			break
		}
		// Decoding from the new base disagrees at the old boundary; absorb it.
		kept = kept[1:]
	}
	if len(kept) == 0 {
		list, _, err := countChunks(ctx, source, base, end, end, options)
		return newChunks(list), err
	}
	list := append([]Chunk(nil), kept...)
	if tail := list[len(list)-1].End; tail < end {
		more, _, err := countChunks(ctx, source, tail, end, end, options)
		if err != nil {
			return Chunks{}, err
		}
		list = append(list, more...)
	}
	return newChunks(list), nil
}

// countChunks counts [start, end) in parallel, decoding no further than limit.
// Interior boundaries are verified and merged on failure; ok reports whether
// end is a verified restart point (always true when end == limit).
func countChunks(ctx context.Context, source Source, start, end, limit int64, options Options) ([]Chunk, bool, error) {
	if start >= end {
		return nil, true, nil
	}
	size := options.ChunkSize
	if size <= 0 {
		size = DefaultChunkSize
	}
	bounds := []int64{start}
	for next := start + size; next < end; {
		boundary, err := lineStart(source, next, end)
		if err != nil {
			return nil, false, err
		}
		if boundary < 0 {
			break
		}
		bounds = append(bounds, boundary)
		next = boundary + size
	}
	bounds = append(bounds, end)
	type scanned struct {
		chunk     Chunk
		straddled bool
	}
	scan := func(start, end int64) (scanned, error) {
		boundary := int64(0)
		if end < limit {
			boundary = end
		}
		result := scanned{chunk: Chunk{Start: start, End: end}}
		var err error
		result.straddled, result.chunk.open, err = walkChecked(ctx, source, options.Query, start, limit, end-1, boundary, func(int64, int64) bool {
			result.chunk.Count++
			return true
		})
		return result, err
	}
	results := make([]scanned, len(bounds)-1)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var (
		wait     sync.WaitGroup
		mu       sync.Mutex
		next     int
		firstErr error
	)
	for range max(1, min(options.Workers, len(results))) {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for {
				mu.Lock()
				i := next
				next++
				mu.Unlock()
				if i >= len(results) || ctx.Err() != nil {
					return
				}
				result, err := scan(bounds[i], bounds[i+1])
				if err != nil {
					mu.Lock()
					if firstErr == nil {
						firstErr = err
					}
					mu.Unlock()
					cancel()
					return
				}
				results[i] = result
			}
		}()
	}
	wait.Wait()
	if firstErr != nil {
		return nil, false, firstErr
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	chunks := func() []Chunk {
		list := make([]Chunk, len(results))
		for i, result := range results {
			list[i] = result.chunk
		}
		return list
	}
	// A token rarely spans a raw newline (for example, one inside an OSC
	// string). Merge such a chunk with its successor and rescan the union.
	for i := 0; i < len(results); {
		if !results[i].straddled {
			i++
			continue
		}
		if i == len(results)-1 {
			return chunks(), false, nil
		}
		merged, err := scan(results[i].chunk.Start, results[i+1].chunk.End)
		if err != nil {
			return nil, false, err
		}
		results[i] = merged
		results = append(results[:i+1], results[i+2:]...)
	}
	return chunks(), true, nil
}

// lineStart returns the offset after the first raw newline in [from, end), or
// -1. It is only a candidate restart point until a chunk scan verifies it.
func lineStart(source Source, from, end int64) (int64, error) {
	// Most lines are short; reading 64 KiB at every boundary of a large file
	// would copy a sizable fraction of it.
	var small [4 << 10]byte
	buffer := small[:]
	for from < end {
		n, err := source.ReadAt(buffer[:min(int64(len(buffer)), end-from)], from)
		if i := bytes.IndexByte(buffer[:n], '\n'); i >= 0 {
			if boundary := from + int64(i) + 1; boundary < end {
				return boundary, nil
			}
			return -1, nil
		}
		from += int64(n)
		if len(buffer) == len(small) {
			buffer = make([]byte, 64<<10)
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return 0, err
		}
		if n == 0 {
			break
		}
	}
	return -1, nil
}

// FindInChunks has FindFrom's semantics within the chunks' snapshot, but skips
// chunks without matches, so a distant or wrapped match costs at most a scan
// of the origin's chunk and of the chunk holding the match. anchor is a known
// restart point at or before origin, letting nearby matches avoid a chunk scan.
func FindInChunks(ctx context.Context, source Source, query Query, chunks Chunks, origin, anchor int64, backward, include bool) (Result, error) {
	list := chunks.List
	if chunks.Total() == 0 {
		return Result{}, nil
	}
	limit := chunks.End()
	origin = min(max(chunks.Start(), origin), limit)
	c := chunks.Find(min(origin, limit-1))
	anchor = min(max(list[c].Start, anchor), origin)
	eligible := func(offset int64) bool {
		if backward {
			return offset < origin || include && offset == origin
		}
		return offset > origin || include && offset == origin
	}
	// first and last return a chunk's first/last match through lastStart.
	first := func(start, through int64, want func(int64) bool) (Result, error) {
		var found Result
		err := WalkRange(ctx, source, query, start, through, limit, func(offset, _ int64) bool {
			if want(offset) {
				found = Result{Offset: offset, Found: true}
				return false
			}
			return true
		})
		return found, err
	}
	last := func(start, through int64, want func(int64) bool) (Result, error) {
		var found Result
		err := WalkRange(ctx, source, query, start, through, limit, func(offset, _ int64) bool {
			if want(offset) {
				found = Result{Offset: offset, Found: true}
			}
			return true
		})
		return found, err
	}
	every := func(int64) bool { return true }
	wrapped := func(found Result, err error) (Result, error) {
		found.Wrapped = found.Found
		return found, err
	}
	if !backward {
		if found, err := first(anchor, list[c].End-1, eligible); found.Found || err != nil {
			return found, err
		}
		for j := c + 1; j < len(list); j++ {
			if list[j].Count > 0 {
				return first(list[j].Start, list[j].End-1, every)
			}
		}
		for j := 0; j <= c; j++ {
			if list[j].Count > 0 {
				return wrapped(first(list[j].Start, list[j].End-1, every))
			}
		}
		return Result{}, nil
	}
	through := min(origin, list[c].End-1)
	if found, err := last(anchor, through, eligible); found.Found || err != nil {
		return found, err
	}
	if anchor > list[c].Start {
		if found, err := last(list[c].Start, anchor-1, eligible); found.Found || err != nil {
			return found, err
		}
	}
	for j := c - 1; j >= 0; j-- {
		if list[j].Count > 0 {
			return last(list[j].Start, list[j].End-1, every)
		}
	}
	for j := len(list) - 1; j >= c; j-- {
		if list[j].Count > 0 {
			return wrapped(last(list[j].Start, list[j].End-1, every))
		}
	}
	return Result{}, nil
}
