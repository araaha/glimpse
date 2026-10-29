package app

import (
	"context"
	"runtime"
	"sync"

	"glimpse/internal/search"
)

// newlines matches every decoded newline, so its chunked parallel count skips
// newlines hidden inside escape strings, exactly as the line index does.
var newlines = search.Query{Text: "\n", Literal: true}

// lineResult counts the lines of a snapshot as ui.Text.LineCount does: one
// per newline, plus a final line if any bytes follow the last newline.
type lineResult struct {
	key        countKey
	generation uint64
	chunks     search.Chunks
	lines      int64
	err        error
}

// lineCounts totals lines far ahead of the display index, which also decodes
// styles, graphemes, and wrapping. All state except the immutable result
// belongs to the UI event loop.
type lineCounts struct {
	results    chan lineResult
	workers    sync.WaitGroup
	cancel     context.CancelFunc
	generation uint64
	running    bool
	task       countKey
	last       *lineResult
}

func newLineCounts() *lineCounts { return &lineCounts{results: make(chan lineResult, 1)} }

func (c *lineCounts) stop() {
	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}
	c.generation++
	c.running = false
}

func (c *lineCounts) close() { c.stop(); c.workers.Wait() }

// request counts the snapshot named by key, reusing the previous chunks of
// the same content so that appended input only rescans the tail. A running
// count of the same content finishes first.
func (c *lineCounts) request(ctx context.Context, source search.Source, key countKey) {
	if c.last != nil && c.last.key == key || c.running && c.task == key {
		return
	}
	var previous search.Chunks
	if c.last != nil && c.last.err == nil && c.last.key.generation == key.generation {
		previous = c.last.chunks
	}
	if c.running {
		if c.task.generation == key.generation && c.task.base == key.base {
			return
		}
		c.stop()
	}
	ctx, c.cancel = context.WithCancel(ctx)
	c.running, c.task = true, key
	generation := c.generation
	c.workers.Add(1)
	go func() {
		defer c.workers.Done()
		result := lineResult{key: key, generation: generation}
		source := countSnapshot{Source: source, key: key}
		options := search.Options{Query: newlines, Workers: max(1, runtime.GOMAXPROCS(0)-1)}
		result.chunks, result.err = search.UpdateChunks(ctx, source, previous, key.base, key.end, options)
		if result.err == nil {
			result.lines, result.err = countLines(ctx, source, result.chunks, key)
		}
		select {
		case c.results <- result:
		case <-ctx.Done():
		}
	}()
}

// countLines adds the final line unless the snapshot ends with a newline.
// Chunks start after raw newlines, so finding the last decoded newline scans
// at most the last chunk holding one.
func countLines(ctx context.Context, source search.Source, chunks search.Chunks, key countKey) (int64, error) {
	lastEnd := key.base
	for i := len(chunks.List) - 1; i >= 0; i-- {
		chunk := chunks.List[i]
		if chunk.Count == 0 {
			continue
		}
		err := search.WalkRange(ctx, source, newlines, chunk.Start, chunk.End-1, key.end, func(_, end int64) bool {
			lastEnd = end
			return true
		})
		if err != nil {
			return 0, err
		}
		break
	}
	lines := chunks.Total()
	if key.end > lastEnd {
		lines++
	}
	return lines, nil
}

func (c *lineCounts) accept(result lineResult) bool {
	if result.generation != c.generation {
		return false
	}
	c.running = false
	c.last = &result
	return true
}

// total returns the line count of the snapshot named by key, if counted.
func (c *lineCounts) total(key countKey) (int64, bool) {
	if c.last == nil || c.last.key != key || c.last.err != nil {
		return 0, false
	}
	return c.last.lines, true
}
