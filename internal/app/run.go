package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"glimpse/internal/keybind"
	"glimpse/internal/ui"

	"github.com/gdamore/tcell/v3"
)

const sequenceTimeout = 90 * time.Millisecond
const indexSlice = 2 * time.Millisecond

// watchInterval is how often a regular file is checked for appends, truncation,
// and (with follow-name) replacement.
const watchInterval = 100 * time.Millisecond

// Run owns input and the terminal until quit, cancellation, or an error. Closing
// input must unblock a pending Read (as it does for files and pipes). name labels
// the input in the statusline and is empty for stdin.
func (a *App) Run(ctx context.Context, input io.ReadCloser, name string) error {
	// Optional capability probing can wait up to a second for a terminal reply.
	// Known terminal profiles and standard keyboard reporting suffice for now.
	screen, err := tcell.NewScreen(tcell.OptNegotiation(false))
	if err != nil {
		input.Close()
		a.Document.Close()
		return fmt.Errorf("create terminal: %w", err)
	}
	return a.run(ctx, input, name, screen)
}

func (a *App) run(ctx context.Context, input io.ReadCloser, name string, screen tcell.Screen) (runErr error) {
	if err := screen.Init(); err != nil {
		input.Close()
		a.Document.Close()
		return fmt.Errorf("initialize terminal: %w", err)
	}
	defer screen.Fini()
	screen.EnablePaste()
	a.truecolor = screen.Colors() >= 1<<24
	if a.Renderer == nil {
		a.Renderer = ui.NewTerminal(screen)
	}
	finished := make(chan struct{})
	result := make(chan error, 1)
	watchCtx, stopWatch := context.WithCancel(ctx)
	var watchers sync.WaitGroup
	go func() {
		defer close(finished)
		result <- a.Ingest(input)
	}()
	defer func() {
		stopWatch()
		watchers.Wait()
		runErr = errors.Join(runErr, input.Close(), a.Document.Close())
		<-finished
	}()

	s, err := a.newSession(ctx, screen, name)
	if err != nil {
		return err
	}
	defer func() { runErr = errors.Join(runErr, s.close()) }()
	tick := time.NewTicker(time.Second / 60)
	defer tick.Stop()

	// Paint immediately rather than waiting for the first frame tick.
	if err := s.draw(a.Document.Len()); err != nil {
		return err
	}
	s.dirty = false
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-s.indexWork.C:
			if err := s.updateText(); err != nil {
				return err
			}
			s.scheduleIndex()
		case err := <-result:
			result = nil
			s.reading, s.dirty = false, true
			if err != nil {
				return fmt.Errorf("read input: %w", err)
			}
			// Regular files are read in place; keep watching them for appends,
			// truncation, and (with follow-name) replacement.
			if file, ok := input.(*os.File); ok && a.Document.FileBacked() {
				watchers.Add(1)
				go func() {
					defer watchers.Done()
					a.Document.Watch(watchCtx, file, name, a.followName, watchInterval)
				}()
			}
		case response := <-s.search.results:
			s.searchDone(response)
		case failure := <-s.runFailures:
			s.search.message, s.dirty = failure, true
		case response := <-s.counts.results:
			if s.counts.accept(response) {
				s.dirty = true
			}
		case result := <-s.lineTotals.results:
			if s.lineTotals.accept(result) {
				s.dirty = true
			}
		case event, ok := <-screen.EventQ():
			if !ok {
				return nil
			}
			if quit, err := s.handle(event); quit || err != nil {
				return err
			}
		case now := <-tick.C:
			if quit, err := s.tick(now); quit || err != nil {
				return err
			}
		}
	}
}

type viewState struct {
	top    int
	height int
	follow bool
	bottom bool
}

func (v *viewState) reflow(anchor, rows, height int, newData bool) {
	v.top = anchor
	if v.bottom || (v.follow && newData) {
		v.top = max(0, rows-max(1, height))
	}
	v.bottom = false
	// Expanding the viewport at EOF must reveal preceding text, not an empty
	// row. Elsewhere, keep the byte-anchored top row unchanged.
	if height > v.height {
		v.top = min(v.top, max(0, rows-max(1, height)))
	}
	v.height = height
	v.top = min(max(0, v.top), max(0, rows-1))
}

func (v *viewState) scroll(delta, rows, height int) {
	target := v.top + delta
	if delta > 0 {
		// After expanding the viewport at EOF, down must not move upward just
		// because the last full-screen starting position is now earlier.
		target = min(target, max(v.top, max(0, rows-max(1, height))))
	}
	v.top = max(0, target)
}

func (v *viewState) apply(action keybind.Action, rows, height int) {
	page := max(1, height)
	v.bottom = false
	switch action {
	case keybind.Follow:
		v.follow = !v.follow
		if v.follow {
			// Update the position now so another action in this event/frame
			// navigates from the end, not from the pre-follow position.
			v.top = max(0, rows-page)
			v.bottom = true // Still catch up with new data before repainting.
		}
		return
	case keybind.Top:
		v.top = 0
	case keybind.Bottom:
		v.top = max(0, rows-page)
		v.bottom = true
	case keybind.Up:
		v.scroll(-1, rows, height)
	case keybind.Down:
		v.scroll(1, rows, height)
	case keybind.PageUp:
		v.scroll(-page, rows, height)
	case keybind.PageDown:
		v.scroll(page, rows, height)
	case keybind.HalfPageUp:
		v.scroll(-max(1, page/2), rows, height)
	case keybind.HalfPageDown:
		v.scroll(max(1, page/2), rows, height)
	default:
		return
	}
	v.follow = false
	v.top = min(v.top, max(0, rows-1))
}
