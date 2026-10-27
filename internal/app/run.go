package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"glimpse/internal/keybind"
	"glimpse/internal/pager"
	"glimpse/internal/ui"

	"github.com/gdamore/tcell/v3"
)

const sequenceTimeout = 90 * time.Millisecond
const indexSlice = 2 * time.Millisecond

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
		runErr = errors.Join(runErr, input.Close(), a.Document.Close())
		<-finished
	}()

	tick := time.NewTicker(time.Second / 60)
	defer tick.Stop()
	indexWork := time.NewTimer(0)
	defer indexWork.Stop()
	text, err := ui.NewTextWithLimits(a.Document, a.metadataBudget, a.metadataQuota)
	if err != nil {
		return err
	}
	defer func() { runErr = errors.Join(runErr, text.Close()) }()
	text.SetWideAmbiguous(a.wideAmbiguous)
	search := searchState{results: make(chan searchResponse, 1), echo: a.UI.CmdHeight() != ui.CmdHeightToggle}
	defer search.close()
	counts := newStatusCounts()
	defer counts.close()
	view := viewState{follow: a.follow}
	reading, dirty := true, true
	var previousLength int64 = -1
	previousRows, indexedWidth := 0, 0
	var previousIndexed int64 = -1
	pendingLine := 0
	var pendingOffset int64
	var bottomEnd int64
	// pendingSearch marks a pending offset as a search match, not a reflow anchor.
	hasPendingOffset, wantBottom, pendingSearch := false, false, false
	previousPaused := false
	var deadline time.Time
	pasting := false
	layout := a.UI.Layout(screen.Size())
	var bodyLines []string
	var bodyStyled [][]ui.Glyph
	var bodyLayout ui.Layout
	var bodyOffset, bodyBase int64
	updateText := func() error {
		anchor := text.Offset(view.top)
		reflow := indexedWidth != layout.Body.Width
		if reflow && indexedWidth != 0 && pendingLine == 0 && !hasPendingOffset && !wantBottom {
			pendingOffset, hasPendingOffset, pendingSearch = anchor, true, false
		}
		if text.NeedsUpdate(layout.Body.Width, !reading) {
			if err := text.UpdateFor(layout.Body.Width, !reading, indexSlice); err != nil {
				return err
			}
			indexedWidth = layout.Body.Width
			if reflow {
				view.top = text.RowAt(anchor)
			}
			dirty = true
		}
		pending := text.NeedsUpdate(layout.Body.Width, !reading)
		if pendingLine > 0 && (pendingLine <= text.LineCount() || !pending) {
			view.top = text.LineRow(pendingLine)
			pendingLine = 0
		}
		if hasPendingOffset && (pendingOffset < text.Indexed() || !pending) {
			view.top = text.RowAt(pendingOffset)
			hasPendingOffset = false
		}
		return nil
	}
	scheduleIndex := func() {
		if text.NeedsUpdate(layout.Body.Width, !reading) {
			indexWork.Reset(0)
		}
	}
	jumpBottom := func() {
		pendingLine, hasPendingOffset = 0, false
		view.follow, view.bottom = false, false
		wantBottom = text.NeedsUpdate(layout.Body.Width, !reading)
		if wantBottom {
			bottomEnd = a.Document.Len()
		} else {
			view.apply(keybind.Bottom, text.RowCount(), layout.Body.Height)
		}
	}

	draw := func(length int64) error {
		paused, discarded := a.Document.Paused(), a.Document.Start()
		a.UI.SetSearchVisible(search.prompt != nil || search.message != "" || paused || discarded > 0)
		layout = a.UI.Layout(screen.Size())
		if err := updateText(); err != nil {
			return err
		}
		if wantBottom && (text.Indexed() >= bottomEnd || !text.NeedsUpdate(layout.Body.Width, !reading)) {
			// Resolve only with the final layout, just before painting. G targets
			// the input available when requested, not each successive indexed tail.
			rows := min(text.RowCount(), text.RowAt(max(0, bottomEnd-1))+1)
			view.top = max(0, rows-max(1, layout.Body.Height))
			view.bottom, wantBottom = false, false
		}
		anchor := text.Offset(view.top)
		view.reflow(text.RowAt(anchor), text.RowCount(), layout.Body.Height, length != previousLength || text.RowCount() != previousRows || text.Indexed() != previousIndexed)
		top := text.Offset(view.top)
		// While G is pending, only progress/status changes. Reuse an already
		// complete, immutable starting viewport instead of decoding it at 60 Hz.
		cachedBody := wantBottom && bodyLayout == layout && bodyOffset == top && bodyBase == discarded && view.top+layout.Body.Height < previousRows
		lines, styled := bodyLines, bodyStyled
		if !cachedBody {
			lines, err = text.Lines(view.top, layout.Body.Height)
			if err != nil {
				if errors.Is(err, pager.ErrEvicted) {
					return nil
				}
				return err
			}
			styled = text.Styled()
		}
		indexing := text.NeedsUpdate(layout.Body.Width, !reading)
		target := top
		if search.matched {
			target = search.match
		}
		key := countKey{query: search.query, smart: a.SmartCase, ignore: a.IgnoreCase, base: discarded, end: a.Document.Len()}
		state := ui.StatusState{
			Mode: "VIEW", Filename: name, HasSearch: search.query != "",
			TopLine: text.LogicalLineAt(top), TotalLines: text.LineCount(), TotalLinesKnown: !reading && !indexing,
		}
		state.SearchCurrent, state.SearchTotal, state.SearchCurrentKnown, state.SearchTotalKnown = counts.values(key, target, search.matched)
		switch {
		case search.busy || search.prompt != nil && !search.prompt.command || pendingSearch && hasPendingOffset:
			state.Mode = "SEARCH"
		case view.follow:
			state.Mode = "FOLLOW"
		}
		bar := a.Statusline.Format(state, layout.Body.Width, a.wideAmbiguous)
		counts.visible = layout.StatusRow >= 0 && bar.Has(ui.StatusSearchResults)
		counts.update(ctx, a.Document, key, target, search.matched, search.busy || hasPendingOffset)
		frame := ui.Frame{
			Layout: layout, Lines: lines, Styled: styled,
			Status: bar.Text(), StatusBar: bar, StatusState: state,
			Search: search.message, WideAmbiguous: a.wideAmbiguous, HighlightStyle: a.SearchHighlight,
			Reading: reading, Indexing: indexing, SeekingEnd: wantBottom, Follow: view.follow, DocumentLength: a.Document.Len(),
		}
		if paused && search.prompt == nil {
			frame.Search = "Storage paused: spool quota reached; :set spool-quota=BYTES to resume"
		}
		if discarded > 0 && search.prompt == nil {
			frame.Search += fmt.Sprintf(" [rolling history: %d bytes discarded]", discarded)
		}
		if a.HighlightSearch {
			frame.Highlights = text.Highlights(lines, view.top, search.query, a.SmartCase, a.IgnoreCase)
		}
		if search.prompt != nil {
			prefix := "/"
			if search.prompt.backward {
				prefix = "?"
			}
			if search.prompt.command {
				prefix = ":"
			}
			frame.Search = prefix + string(search.prompt.text)
			frame.SearchActive = true
			frame.SearchCursor = 1 + search.prompt.cursor
		}
		if err := a.Renderer.Render(frame); err != nil {
			return err
		}
		bodyLines, bodyStyled, bodyLayout = lines, styled, layout
		bodyOffset, bodyBase = top, discarded
		previousLength = length
		previousRows, previousIndexed = text.RowCount(), text.Indexed()
		previousPaused = paused
		scheduleIndex()
		return nil
	}
	var applyPrompt func([]keybind.Command) (bool, error)
	apply := func(actions []keybind.Command) (bool, error) {
		// Discard unresolved actions before cancellation, not commands retried
		// afterward. The trie has already consumed the canceled prefix; any
		// pending input now belongs to a new command and must be preserved.
		for i := len(actions) - 1; i >= 0; i-- {
			if actions[i].Action == keybind.Cancel {
				pendingLine, hasPendingOffset, wantBottom = 0, false, false
				search.stop()
				search.message = ""
				dirty = true
				actions = actions[i+1:]
				break
			}
		}
		for _, command := range actions {
			action := command.Action
			switch action {
			case keybind.SearchForward, keybind.SearchBackward, keybind.CommandInput:
				pendingLine, hasPendingOffset, wantBottom = 0, false, false
				search.begin(action == keybind.SearchBackward)
				if action == keybind.CommandInput {
					search.prompt = newPrompt(false, search.commandHistory.Entries)
					search.prompt.command = true
				}
				search.prompt.origin = text.Offset(view.top)
				view.follow, view.bottom = false, false
				a.Keys.Cancel()
				a.SearchKeys.Cancel()
				for _, key := range command.Remainder {
					if quit, err := applyPrompt(a.SearchKeys.Feed(key)); quit || err != nil {
						return quit, err
					}
				}
				dirty = true
				return false, nil
			case keybind.SearchNext, keybind.SearchPrevious:
				pendingLine, hasPendingOffset, wantBottom = 0, false, false
				if search.query == "" {
					search.message = "No previous search"
				} else {
					origin, include := text.Offset(view.top), true
					if search.matched {
						origin, include = search.match, false
					}
					backward := search.backward
					if action == keybind.SearchPrevious {
						backward = !backward
					}
					view.follow, view.bottom = false, false
					if search.busy {
						search.repeats = append(search.repeats, backward)
					} else {
						search.start(ctx, a.Document, origin, text.SearchAnchor(origin), backward, a.SmartCase, a.IgnoreCase, include)
					}
				}
				dirty = true
				continue
			}
			if action == keybind.Quit {
				return true, nil
			}
			if action == keybind.ToggleStatus {
				a.UI.ToggleStatusline()
			} else if action == keybind.Bottom && command.Count > 0 {
				pendingLine, hasPendingOffset, wantBottom = command.Count, false, false
				view.follow, view.bottom = false, false
			} else if action == keybind.Bottom {
				jumpBottom()
			} else {
				pendingLine, hasPendingOffset, wantBottom = 0, false, false
				view.apply(action, text.RowCount(), layout.Body.Height)
			}
			search.stop()
			search.matched, search.message = false, ""
			dirty = true
		}
		return false, nil
	}
	preview := func() {
		p := search.prompt
		if p == nil || p.command || !a.IncrementalSearch {
			return
		}
		pendingLine, hasPendingOffset, wantBottom = 0, false, false
		search.stop()
		query := string(p.text)
		if query == "" {
			search.query, search.backward = search.original.query, search.original.backward
			search.match, search.matched = search.original.match, search.original.matched
			search.message = ""
			view.top = text.RowAt(p.origin)
			return
		}
		search.query, search.backward, search.matched = query, p.backward, false
		search.start(ctx, a.Document, p.origin, text.SearchAnchor(p.origin), p.backward, a.SmartCase, a.IgnoreCase, true)
	}
	applyPrompt = func(commands []keybind.Command) (bool, error) {
		for i := len(commands) - 1; i >= 0; i-- {
			if commands[i].Action == keybind.SearchCancel {
				commands = commands[i:]
				break
			}
		}
		for _, command := range commands {
			if search.prompt == nil {
				break
			}
			switch command.Action {
			case keybind.SearchCancel:
				pendingLine, hasPendingOffset, wantBottom = 0, false, false
				search.stop()
				view.top = text.RowAt(search.prompt.origin)
				search.query, search.backward = search.original.query, search.original.backward
				search.match, search.matched = search.original.match, search.original.matched
				search.prompt = nil
				search.message = ""
				a.SearchKeys.Cancel()
			case keybind.Accept:
				query := string(search.prompt.text)
				backward := search.prompt.backward
				origin := search.prompt.origin
				isCommand := search.prompt.command
				search.prompt = nil
				a.SearchKeys.Cancel()
				if isCommand {
					result, err := a.command(query)
					if err != nil {
						search.message = err.Error()
					} else {
						if result.quit {
							return true, nil
						}
						search.message = result.message
						if result.line > 0 {
							search.matched = false
							pendingLine, hasPendingOffset, wantBottom = result.line, false, false
						} else if result.action != "" {
							search.matched = false
							if result.action == keybind.Bottom {
								jumpBottom()
							} else {
								pendingLine, hasPendingOffset, wantBottom = 0, false, false
								view.apply(result.action, text.RowCount(), layout.Body.Height)
							}
						}
						if query != "" {
							if err := search.commandHistory.Add(query); err != nil {
								search.message += fmt.Sprintf(" [history: %v]", err)
							}
						}
					}
					break
				}
				if query == "" {
					query = search.original.query
				}
				if query == "" {
					search.message = "No previous search"
					break
				}
				if err := search.history.Add(query); err != nil {
					search.warning = fmt.Sprintf("history: %v", err)
				}
				search.query, search.backward, search.matched = query, backward, false
				view.follow, view.bottom = false, false
				search.start(ctx, a.Document, origin, text.SearchAnchor(origin), backward, a.SmartCase, a.IgnoreCase, true)
			default:
				before := string(search.prompt.text)
				search.prompt.edit(command)
				if string(search.prompt.text) != before {
					preview()
				}
			}
			dirty = true
			if command.Action == keybind.Accept || command.Action == keybind.SearchCancel {
				for _, key := range command.Remainder {
					if quit, err := apply(a.Keys.Feed(key)); quit || err != nil {
						return quit, err
					}
				}
				return false, nil
			}
		}
		return false, nil
	}
	dispatchKeys := func(keys []keybind.Key) (bool, error) {
		var batches []*prompt
		defer func() {
			for _, p := range batches {
				p.batching = false
				p.cursor = p.align(p.cursor, true)
			}
		}()
		for _, key := range keys {
			if search.prompt != nil {
				if len(keys) > 1 && !search.prompt.batching {
					search.prompt.batching = true
					batches = append(batches, search.prompt)
				}
				if quit, err := applyPrompt(a.SearchKeys.Feed(key)); quit || err != nil {
					return quit, err
				}
			} else if quit, err := apply(a.Keys.Feed(key)); quit || err != nil {
				return quit, err
			}
		}
		return false, nil
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
		case <-indexWork.C:
			if err := updateText(); err != nil {
				return err
			}
			scheduleIndex()
		case err := <-result:
			result = nil
			reading, dirty = false, true
			if err != nil {
				return fmt.Errorf("read input: %w", err)
			}
		case response := <-search.results:
			if response.generation != search.generation {
				continue
			}
			search.busy = false
			if response.result.Found && !response.available(a.Document.Start(), a.Document.Len()) {
				response.result.Found = false
				response.err = fmt.Errorf("match was discarded by rolling retention; repeat to search available history")
				search.matched = false
			}
			if response.err != nil {
				search.message = fmt.Sprintf("Search: %v", response.err)
			} else if !response.result.Found {
				search.message = "Pattern not found: " + search.query
				search.matched = false
			} else {
				search.match, search.matched = response.result.Offset, true
				pendingOffset, hasPendingOffset, pendingSearch, pendingLine, wantBottom = search.match, true, true, 0, false
				if search.match < text.Indexed() {
					view.top = text.RowAt(search.match)
					hasPendingOffset = false
				}
				view.follow, view.bottom = false, false
				search.message = search.echoQuery()
				if response.result.Wrapped && search.echo {
					search.message += "  [wrapped]"
				}
			}
			if search.warning != "" {
				search.message = strings.TrimSpace(search.message + "  [" + search.warning + "]")
			}
			if response.err == nil && response.result.Found && len(search.repeats) > 0 {
				backward := search.repeats[0]
				search.repeats = search.repeats[1:]
				search.start(ctx, a.Document, search.match, text.SearchAnchor(search.match), backward, a.SmartCase, a.IgnoreCase, false)
			} else if response.err != nil || !response.result.Found {
				search.repeats = nil
			}
			dirty = true
		case response := <-counts.results:
			if counts.accept(response) {
				dirty = true
			}
		case event, ok := <-screen.EventQ():
			if !ok {
				return nil
			}
			switch ev := event.(type) {
			case *tcell.EventPaste:
				if ev.Start() {
					a.Keys.Cancel()
					if search.prompt != nil {
						if quit, err := applyPrompt(a.SearchKeys.DrainLiteral()); quit || err != nil {
							return err
						}
					}
				}
				pasting = ev.Start()
			case *tcell.EventResize:
				screen.Sync()
				dirty = true
			case *tcell.EventError:
				return fmt.Errorf("terminal: %w", ev)
			case *tcell.EventKey:
				if pasting {
					if search.prompt != nil && ev.Pressed() && ev.Key() == tcell.KeyRune {
						search.prompt.edit(keybind.Command{Action: keybind.Insert, Text: ev.Str()})
						preview()
						dirty = true
					}
					continue
				}
				keys := ui.KeysFromEvent(ev)
				if len(keys) == 0 {
					continue
				}
				if quit, err := dispatchKeys(keys); quit || err != nil {
					return err
				}
				deadline = time.Now().Add(sequenceTimeout)
			}
		case now := <-tick.C:
			// A throttled recount may become due even when the viewport is idle.
			if !search.busy && search.query != "" && counts.recountDue(now, a.Document.Len()) {
				dirty = true
			}
			if search.prompt != nil && a.SearchKeys.NeedsTimeout() && !now.Before(deadline) {
				if quit, err := applyPrompt(a.SearchKeys.Flush()); quit || err != nil {
					return err
				}
			}
			if a.Keys.NeedsTimeout() && !now.Before(deadline) {
				if quit, err := apply(a.Keys.Flush()); quit || err != nil {
					return err
				}
			}
			length := a.Document.Len()
			if dirty || length != previousLength || a.Document.Paused() != previousPaused {
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
