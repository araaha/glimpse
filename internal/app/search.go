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

func (s *searchState) start(ctx context.Context, source search.Source, origin int64, backward, smartCase, ignoreCase, include bool) {
	repeats := s.repeats
	s.stop()
	s.repeats, s.busy = repeats, true
	ctx, s.cancel = context.WithCancel(ctx)
	generation, query := s.generation, s.query
	s.message = "Searching…"
	s.workers.Add(1)
	go func() {
		defer s.workers.Done()
		result, err := search.Find(ctx, source, query, origin, backward, smartCase, ignoreCase, include)
		select {
		case s.results <- searchResponse{generation, result, err}:
		case <-ctx.Done():
		}
	}()
}
