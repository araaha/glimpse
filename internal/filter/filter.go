// Package filter presents the lines of a document that match a query as a
// document of their own, which grows as the matching lines are found.
package filter

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/araaha/glimpse/internal/pager"
	"github.com/araaha/glimpse/internal/search"
)

// reset starts every segment, so a style left open by a hidden line does not
// color the next line shown. It is decoded as a hidden control.
const reset = "\x1b[m"

// segment is a run of consecutive matching lines: real bytes of the document
// shown at virtual offsets after a reset.
type segment struct {
	virtual, real, length int64 // length counts the real bytes
	line, vline, lines    int64 // the first real and virtual line numbers, and the count
}

func (s segment) realEnd() int64 { return s.real + s.length }

// segmentBytes is the memory a segment takes, counted against the quota.
const segmentBytes = 48

// Source is the filtered view of a document: its matching lines, in order. It
// is safe for concurrent readers while Run appends to it.
type Source struct {
	document search.Source
	query    search.Query
	options  search.Options
	quota    int64 // the most bytes of segments to keep

	mu          sync.RWMutex
	segments    []segment
	length      int64
	decided     int64 // the matching lines before this real offset are known
	decidedLine int64 // the real line number at decided
	final       bool  // the whole document is decided
	err         error

	complete atomic.Bool // the document will not grow, except by being watched
	wake     chan struct{}

	// Owned by Run.
	scanned int64 // the first undecided line, or the line still growing
	line    int64 // the real line number at scanned
	open    bool  // the last segment ends with a line that may still grow
}

// New filters document from its first retained byte, keeping at most quota
// bytes of segments. Run finds the lines.
func New(document search.Source, query search.Query, options search.Options, quota int64) *Source {
	start := int64(0)
	if retained, ok := document.(interface{ Start() int64 }); ok {
		start = retained.Start()
	}
	return &Source{
		document: document, query: query, options: options, quota: quota,
		decided: start, decidedLine: 1, scanned: start, line: 1,
		wake: make(chan struct{}, 1),
	}
}

// Wake asks Run to look for more input; complete reports that the document's
// input has ended, so its final line is complete even without a newline.
func (s *Source) Wake(complete bool) {
	s.complete.Store(complete)
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// Run finds matching lines until ctx is canceled or an error stops it, scanning
// again whenever Wake is called. A truncated document waits for a reload.
func (s *Source) Run(ctx context.Context) {
	for {
		err := s.update(ctx)
		if err != nil && !errors.Is(err, pager.ErrTruncated) {
			if ctx.Err() == nil {
				s.mu.Lock()
				s.err = err
				s.mu.Unlock()
			}
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-s.wake:
		}
	}
}

// update scans the input available now, a window of chunks at a time, so the
// first matches appear before a large document has been read through.
func (s *Source) update(ctx context.Context) error {
	complete := s.complete.Load()
	end := s.document.Len()
	window := max(s.options.ChunkSize, search.DefaultChunkSize) * int64(max(1, s.options.Workers))
	size := window
	for {
		stop := min(end, s.scanned+size)
		partial := complete && stop == end
		if s.scanned >= stop && !(s.open && partial) {
			break
		}
		scan, err := search.MatchingLines(ctx, s.document, s.query, s.scanned, stop, partial, s.open, s.options)
		if err != nil {
			return err
		}
		if scan.Lines == 0 && !partial {
			if stop == end {
				break // The line is still being written.
			}
			size *= 2 // One line is longer than the window.
			continue
		}
		if err := s.add(scan, stop, partial); err != nil {
			return err
		}
		size = window
		if stop == end {
			break
		}
	}
	s.mu.Lock()
	s.final = complete && s.decided == end
	s.mu.Unlock()
	return nil
}

// add appends the matching lines of a scan from s.scanned to stop.
func (s *Source) add(scan search.LineScan, stop int64, partial bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, run := range scan.Runs {
		lines := run.Lines
		n := len(s.segments)
		if s.open && run.Line == 0 {
			// The last line shown has grown since: extend it.
			last := &s.segments[n-1]
			s.length += run.End - last.realEnd()
			last.length = run.End - last.real
			last.lines += lines - 1
			continue
		}
		if n > 0 && s.segments[n-1].realEnd() == run.Start {
			last := &s.segments[n-1]
			last.length += run.End - run.Start
			last.lines += lines
			s.length += run.End - run.Start
			continue
		}
		if int64(n+1)*segmentBytes > s.quota {
			return fmt.Errorf("too many separate matching lines; increase storage.metadata-quota")
		}
		vline := int64(1)
		if n > 0 {
			vline = s.segments[n-1].vline + s.segments[n-1].lines
		}
		s.segments = append(s.segments, segment{
			virtual: s.length, real: run.Start, length: run.End - run.Start,
			line: s.line + run.Line, vline: vline, lines: lines,
		})
		s.length += int64(len(reset)) + run.End - run.Start
	}
	s.line += scan.Lines
	s.scanned = scan.Tail
	s.open = partial && scan.TailMatched
	s.decided, s.decidedLine = scan.Tail, s.line
	if partial {
		s.decided = stop
	}
	return nil
}

// Len is the length of the filtered view in bytes.
func (s *Source) Len() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.length
}

// Err reports why finding matching lines stopped, if it failed.
func (s *Source) Err() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.err
}

// Final reports that every line of the document has been checked.
func (s *Source) Final() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.final
}

// Count is the number of matching lines found so far.
func (s *Source) Count() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if n := len(s.segments); n > 0 {
		return s.segments[n-1].vline + s.segments[n-1].lines - 1
	}
	return 0
}

// LastLine is the real line number of the last matching line found so far.
func (s *Source) LastLine() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if n := len(s.segments); n > 0 {
		return s.segments[n-1].line + s.segments[n-1].lines - 1
	}
	return 0
}

// find returns the segment holding a virtual offset, or false past the end.
func (s *Source) find(offset int64) (segment, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	i := sort.Search(len(s.segments), func(i int) bool { return s.segments[i].virtual > offset }) - 1
	if i < 0 || offset >= s.length {
		return segment{}, false
	}
	return s.segments[i], true
}

func (s *Source) ReadAt(p []byte, off int64) (int, error) {
	n := 0
	for n < len(p) {
		position := off + int64(n)
		seg, ok := s.find(position)
		if !ok {
			return n, io.EOF
		}
		within := position - seg.virtual
		if within < int64(len(reset)) {
			n += copy(p[n:], reset[within:])
			continue
		}
		real := seg.real + within - int64(len(reset))
		want := int(min(int64(len(p)-n), seg.realEnd()-real))
		m, err := s.document.ReadAt(p[n:n+want], real)
		n += m
		if m < want {
			if err == nil || errors.Is(err, io.EOF) {
				err = io.ErrUnexpectedEOF
			}
			return n, err
		}
	}
	return n, nil
}

// RealOffset maps a virtual offset to the document. A reset maps to the start
// of the line after it, and offsets past the end to the end of the last line.
func (s *Source) RealOffset(offset int64) int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	i := sort.Search(len(s.segments), func(i int) bool { return s.segments[i].virtual > offset }) - 1
	if i < 0 {
		return s.decided
	}
	seg := s.segments[i]
	return seg.real + min(seg.length, max(0, offset-seg.virtual-int64(len(reset))))
}

// VirtualOffset maps a document offset to the matching line holding it, or
// else to the next matching line, or the end. It reports false while the
// lines up to offset are still being checked.
func (s *Source) VirtualOffset(offset int64) (int64, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	i := sort.Search(len(s.segments), func(i int) bool { return s.segments[i].realEnd() > offset })
	if i == len(s.segments) {
		return s.length, offset < s.decided || s.final
	}
	seg := s.segments[i]
	if offset < seg.real {
		return seg.virtual, true
	}
	return seg.virtual + int64(len(reset)) + offset - seg.real, true
}

// RealLine maps a one-based line number of the filtered view to the document.
func (s *Source) RealLine(line int64) int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	i := sort.Search(len(s.segments), func(i int) bool { return s.segments[i].vline > line }) - 1
	if i < 0 {
		return line
	}
	return s.segments[i].line + line - s.segments[i].vline
}

// VirtualLine maps a document line number to the matching line at or after
// it, or past the last one. It reports false while that line is unchecked.
func (s *Source) VirtualLine(line int64) (int64, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	i := sort.Search(len(s.segments), func(i int) bool { return s.segments[i].line+s.segments[i].lines > line })
	if i == len(s.segments) {
		next := int64(1)
		if i > 0 {
			next = s.segments[i-1].vline + s.segments[i-1].lines
		}
		return next, line < s.decidedLine || s.final
	}
	seg := s.segments[i]
	return seg.vline + max(0, line-seg.line), true
}
