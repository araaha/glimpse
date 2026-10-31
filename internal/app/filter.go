package app

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"

	"github.com/araaha/glimpse/internal/filter"
	"github.com/araaha/glimpse/internal/search"
	"github.com/araaha/glimpse/internal/ui"
)

// filterState shows only the lines matching a query. The view then indexes a
// filter.Source instead of the document, and the unfiltered index waits in
// plain to be restored. Positions waiting for the filter to reach them are
// document offsets and lines.
type filterState struct {
	query   string
	source  *filter.Source
	plain   *ui.Text
	id      uint64 // tells count keys of successive filters apart
	cancel  context.CancelFunc
	workers sync.WaitGroup
	origin  int64 // the document offset the filter was set at

	woken         int64 // the document length the worker last heard of
	wokenComplete bool
	reported      bool // a failure or an empty result has been shown

	pendingOffset    int64
	hasPendingOffset bool
	pendingLine      int
	wantBottom       bool // G waits for the last matching line of ended input
}

// filtered reports whether the view shows only matching lines.
func (s *session) filtered() bool { return s.filter.source != nil }

// viewSource is the input the view shows: the matching lines while filtered,
// else the document. Searches, counts, and offsets in the view refer to it.
func (s *session) viewSource() search.Source {
	if s.filtered() {
		return s.filter.source
	}
	return s.Document
}

// viewStart is the first retained offset of viewSource.
func (s *session) viewStart() int64 {
	if s.filtered() {
		return 0
	}
	return s.Document.Start()
}

// realLine maps a line of the view to the document.
func (s *session) realLine(line int) int {
	if !s.filtered() || line <= 0 {
		return line
	}
	return int(s.filter.source.RealLine(int64(line)))
}

// realOffset maps an offset in the view to the document.
func (s *session) realOffset(offset int64) int64 {
	if !s.filtered() {
		return offset
	}
	return s.filter.source.RealOffset(offset)
}

// targetOffset is the document offset at the jump-target row.
func (s *session) targetOffset() int64 {
	if s.filter.hasPendingOffset {
		return s.filter.pendingOffset
	}
	if s.text.RowCount() == 0 && s.filtered() {
		return s.filter.origin
	}
	row := min(s.view.top+s.JumpTarget.Row(s.layout.Body.Height), s.text.RowCount()-1)
	return s.realOffset(s.text.Offset(max(0, row)))
}

// jumpLine goes to a document line once the index reaches it; while filtered,
// to the first matching line at or after it.
func (s *session) jumpLine(line int) {
	if s.filtered() {
		s.filter.pendingLine = line
	} else {
		s.pendingLine = line
	}
}

// setFilter shows only the lines matching query, keeping the line at the
// jump target in view, or the next matching line. An empty query clears it.
func (s *session) setFilter(query string) error {
	if query == "" {
		return s.clearFilter()
	}
	if err := s.query(query).Validate(); err != nil {
		s.search.message = err.Error()
		return nil
	}
	if !s.Document.RetainsAll() {
		s.search.message = "Filtering needs --retention=lossless"
		return nil
	}
	origin, following := s.targetOffset(), s.view.follow
	source := filter.New(s.Document, s.query(query), search.Options{Workers: max(1, runtime.GOMAXPROCS(0)-1)}, s.metadataQuota)
	text, err := s.newText(source)
	if err != nil {
		return err
	}
	if err := s.stopFilter(); err != nil {
		return errors.Join(err, text.Close())
	}
	ctx, cancel := context.WithCancel(s.ctx)
	s.filter.query, s.filter.source, s.filter.cancel, s.filter.plain = query, source, cancel, s.text
	s.filter.id++
	s.filter.origin, s.filter.reported, s.filter.woken = origin, false, -1
	s.filter.workers.Add(1)
	go func() {
		defer s.filter.workers.Done()
		source.Run(ctx)
	}()
	s.wakeFilter()
	s.text = text
	s.showNewText()
	if !following {
		s.filter.pendingOffset, s.filter.hasPendingOffset = origin, true
	}
	s.search.stop()
	s.search.matched, s.search.message = false, "&"+query
	return nil
}

// clearFilter shows every line again, keeping the line at the jump target.
func (s *session) clearFilter() error {
	s.search.message = ""
	if !s.filtered() {
		return nil
	}
	origin, following := s.targetOffset(), s.view.follow
	if err := s.stopFilter(); err != nil {
		return err
	}
	s.showNewText()
	if !following {
		s.pendingOffset, s.hasPendingOffset, s.pendingPlace = origin, true, true
	}
	s.search.stop()
	s.search.matched = false
	return nil
}

// stopFilter ends the filter's worker and restores the unfiltered index.
func (s *session) stopFilter() error {
	if !s.filtered() {
		return nil
	}
	s.filter.cancel()
	s.filter.workers.Wait()
	err := s.text.Close()
	s.text, s.filter.plain, s.filter.source, s.filter.query = s.filter.plain, nil, nil, ""
	return err
}

// showNewText starts the view over after s.text was replaced.
func (s *session) showNewText() {
	s.clearPending()
	s.view.top, s.indexedWidth = 0, 0
	s.body.lines, s.body.styled, s.previousLength = nil, nil, -1
	s.dirty = true
}

// wakeFilter tells the filter's worker about new input.
func (s *session) wakeFilter() {
	length := s.Document.Len()
	if s.filter.woken != length || s.filter.wokenComplete != !s.reading {
		s.filter.woken, s.filter.wokenComplete = length, !s.reading
		s.filter.source.Wake(!s.reading)
	}
}

// updateFilter runs once per frame: it passes new input to the worker,
// reports a failed or empty filter, and resolves positions waiting for it.
func (s *session) updateFilter() {
	if !s.filtered() {
		return
	}
	s.wakeFilter()
	source := s.filter.source
	if !s.filter.reported {
		if err := source.Err(); err != nil {
			s.search.message, s.filter.reported, s.dirty = fmt.Sprintf("Filter: %v", err), true, true
		} else if source.Final() && source.Count() == 0 {
			s.search.message, s.filter.reported, s.dirty = "Pattern not found: &"+s.filter.query, true, true
		}
	}
	if s.filter.hasPendingOffset {
		if offset, ok := source.VirtualOffset(s.filter.pendingOffset); ok {
			s.filter.hasPendingOffset = false
			s.pendingOffset, s.hasPendingOffset, s.pendingPlace = offset, true, true
			s.dirty = true
		}
	}
	if s.filter.pendingLine > 0 {
		if line, ok := source.VirtualLine(int64(s.filter.pendingLine)); ok {
			s.filter.pendingLine, s.pendingLine = 0, int(line)
			s.dirty = true
		}
	}
	if s.filter.wantBottom && source.Final() {
		s.jumpBottom()
		s.dirty = true
	}
}
