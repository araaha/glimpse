package app

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"

	"glimpse/internal/search"
)

type searchResponse struct {
	generation uint64
	result     search.Result
	err        error
}

func (r searchResponse) available(start, end int64) bool {
	return r.result.Offset >= start && r.result.Offset < end
}

// searchState is owned by the UI loop; workers only send immutable results.
type searchState struct {
	prompt         *prompt
	history        search.History
	commandHistory search.History
	loaded         bool
	query          string
	backward       bool
	match          int64
	matched        bool
	busy           bool
	repeats        []bool // Directions queued while a worker is resolving a match.
	message        string
	warning        string
	echo           bool // Repeat the query in the footer; mode -1 shows status instead.
	generation     uint64
	cancel         context.CancelFunc
	workers        sync.WaitGroup
	results        chan searchResponse
	original       searchAnchor
}

type searchAnchor struct {
	query    string
	backward bool
	match    int64
	matched  bool
}

func (s *searchState) stop() {
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.generation++
	s.busy, s.repeats = false, nil
}

func (s *searchState) close() { s.stop(); s.workers.Wait() }

func (s *searchState) begin(backward bool) {
	s.stop()
	s.original = searchAnchor{s.query, s.backward, s.match, s.matched}
	if !s.loaded {
		s.loaded = true
		path, err := search.DefaultHistoryPath()
		s.history.Path = path
		if err == nil {
			err = s.history.Load()
			s.commandHistory.Path = filepath.Join(filepath.Dir(path), "command-history")
			if commandErr := s.commandHistory.Load(); err == nil {
				err = commandErr
			}
		}
		if err != nil {
			s.warning = fmt.Sprintf("history: %v", err)
		}
	}
	s.prompt = newPrompt(backward, s.history.Entries)
	s.message = ""
}

// echoQuery is the footer text for the current query: its direction prefix and
// text, or nothing when the statusline alone reports search progress.
func (s *searchState) echoQuery() string {
	if !s.echo {
		return ""
	}
	if s.backward {
		return "?" + s.query
	}
	return "/" + s.query
}

// start searches in the background. With an exact chunk index for the current
// snapshot, chunks without matches are skipped instead of scanned.
func (s *searchState) start(ctx context.Context, source search.Source, chunks search.Chunks, indexed bool, origin, anchor int64, backward, smartCase, ignoreCase, include bool) {
	repeats := s.repeats
	s.stop()
	s.repeats, s.busy = repeats, true
	ctx, s.cancel = context.WithCancel(ctx)
	generation, query := s.generation, s.query
	s.message = s.echoQuery()
	s.workers.Add(1)
	go func() {
		defer s.workers.Done()
		var result search.Result
		var err error
		if indexed {
			result, err = search.FindInChunks(ctx, source, query, chunks, origin, anchor, backward, smartCase, ignoreCase, include)
		} else {
			result, err = search.FindFrom(ctx, source, query, origin, anchor, backward, smartCase, ignoreCase, include)
		}
		select {
		case s.results <- searchResponse{generation, result, err}:
		case <-ctx.Done():
		}
	}()
}
