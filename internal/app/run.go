package app

import (
	"context"
	"fmt"
	"io"
	"slices"
	"time"

	"glimpse/internal/keybind"
	"glimpse/internal/ui"

	"github.com/gdamore/tcell/v3"
)

const sequenceTimeout = 90 * time.Millisecond

// Run owns input and the terminal until quit, cancellation, or an error. Closing
// input must unblock a pending Read (as it does for files and pipes).
func (a *App) Run(ctx context.Context, input io.ReadCloser, name string) error {
	// Optional capability probing can wait up to a second for a terminal reply.
	// Known terminal profiles and standard keyboard reporting suffice for now.
	screen, err := tcell.NewScreen(tcell.OptNegotiation(false))
	if err != nil {
		input.Close()
		return fmt.Errorf("create terminal: %w", err)
	}
	return a.run(ctx, input, name, screen)
}

func (a *App) run(ctx context.Context, input io.ReadCloser, name string, screen tcell.Screen) error {
	if err := screen.Init(); err != nil {
		input.Close()
		return fmt.Errorf("initialize terminal: %w", err)
	}
	defer screen.Fini()
	if a.Renderer == nil {
		a.Renderer = ui.NewTerminal(screen)
	}
	finished := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		defer close(finished)
		result <- a.Ingest(input)
	}()
	defer func() {
		input.Close() // Unblock a reader waiting on a pipe when the user quits.
		<-finished
	}()

	tick := time.NewTicker(time.Second / 60)
	defer tick.Stop()
	text := ui.NewText(a.Document)
	view := viewState{follow: a.follow}
	reading, dirty := true, true
	var previousLength int64 = -1
	var deadline time.Time
	layout := a.UI.Layout(screen.Size())

	draw := func(length int64) error {
		anchor := text.Offset(view.top)
		previousRows := text.RowCount()
		layout = a.UI.Layout(screen.Size())
		if err := text.Update(layout.Body.Width, !reading); err != nil {
			return err
		}
		view.reflow(text.RowAt(anchor), text.RowCount(), layout.Body.Height, length != previousLength || text.RowCount() != previousRows)
		lines, err := text.Lines(view.top, layout.Body.Height)
		if err != nil {
			return err
		}
		state := ""
		if reading {
			state = "  reading"
		}
		if view.follow {
			state += "  FOLLOW"
		}
		status := fmt.Sprintf("%s  %d bytes  row %d/%d%s", name, a.Document.Len(), min(view.top+1, text.RowCount()), text.RowCount(), state)
		if err := a.Renderer.Render(ui.Frame{Layout: layout, Lines: lines, Status: status}); err != nil {
			return err
		}
		previousLength = length
		return nil
	}
	apply := func(actions []keybind.Action) bool {
		// Cancel discards an unresolved shorter binding as well as its prefix.
		// The key that invokes it is configurable like every other action.
		if slices.Contains(actions, keybind.Cancel) {
			a.Keys.Cancel()
			return false
		}
		for _, action := range actions {
			if action == keybind.Quit {
				return true
			}
			if action == keybind.ToggleStatus {
				a.UI.ToggleStatusline()
			} else {
				view.apply(action, text.RowCount(), layout.Body.Height)
			}
			dirty = true
		}
		return false
	}

	// Paint immediately rather than waiting for the first frame tick.
	if err := draw(a.Document.Len()); err != nil {
		return err
	}
	dirty = false
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-result:
			result = nil
			reading, dirty = false, true
			if err != nil {
				return fmt.Errorf("read input: %w", err)
			}
		case event, ok := <-screen.EventQ():
			if !ok {
				return nil
			}
			switch ev := event.(type) {
			case *tcell.EventResize:
				screen.Sync()
				dirty = true
			case *tcell.EventError:
				return fmt.Errorf("terminal: %w", ev)
			case *tcell.EventKey:
				key, ok := ui.KeyFromEvent(ev)
				if !ok {
					continue
				}
				if apply(a.Keys.Feed(key)) {
					return nil
				}
				deadline = time.Now().Add(sequenceTimeout)
			}
		case now := <-tick.C:
			if a.Keys.NeedsTimeout() && !now.Before(deadline) && apply(a.Keys.Flush()) {
				return nil
			}
			length := a.Document.Len()
			if dirty || length != previousLength {
				if err := draw(length); err != nil {
					return err
				}
				dirty = false
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
		v.bottom = v.follow // Explicitly enabling follow jumps to the end now.
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
