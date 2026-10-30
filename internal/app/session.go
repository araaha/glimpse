package app

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"glimpse/internal/keybind"
	"glimpse/internal/pager"
	"glimpse/internal/search"
	"glimpse/internal/ui"

	"github.com/gdamore/tcell/v3"
)

// session is the state of one Run. Only the UI loop touches it; background
// workers report back through channels.
type session struct {
	*App
	ctx    context.Context
	screen tcell.Screen
	name   string // the path the file watcher reopens; empty for stdin
	// display names the input in the statusline and messages.
	display     string
	displayPath bool

	text             *ui.Text
	loadedGeneration int64
	search           searchState
	counts           *statusCounts
	lineTotals       *lineCounts
	runFailures      chan string // background run commands report failures here
	indexWork        *time.Timer

	layout   ui.Layout
	view     viewState
	left     int // first visible display column of unwrapped rows
	reading  bool
	dirty    bool
	pasting  bool
	deadline time.Time // when a pending key sequence times out

	help     []string // the help screen's lines while it is open
	helpView viewState

	// A jump waits here until the index reaches its target.
	pendingLine      int
	pendingOffset    int64
	hasPendingOffset bool
	pendingSearch    bool // the pending offset is a search match, not a reflow anchor
	wantBottom       bool
	bottomEnd        int64

	// The gutter holds line numbers plus a space. Its digit count only grows,
	// because a wider gutter narrows wrapped text and restarts the line index.
	gutterDigits int
	indexedWidth int
	indexedWrap  bool

	// The last painted frame.
	previousLength      int64
	previousRows        int
	previousIndexed     int64
	previousPaused      bool
	previousWatchStatus string
	body                bodyCache
}

// bodyCache keeps the last painted rows, reused while G waits for the index.
type bodyCache struct {
	lines  []string
	styled [][]ui.Glyph
	layout ui.Layout
	offset int64
	base   int64
	left   int
}

func (a *App) newSession(ctx context.Context, screen tcell.Screen, name string) (*session, error) {
	s := &session{
		App: a, ctx: ctx, screen: screen, name: name, display: a.Label,
		view: viewState{follow: a.follow}, reading: true, dirty: true,
		layout: a.UI.Layout(screen.Size()), indexedWrap: a.Wrap,
		previousLength: -1, previousIndexed: -1,
	}
	if s.display == "" && name != "" {
		s.display, s.displayPath = ui.FormatFilename(name, a.FilenameStyle), true
	}
	var err error
	if s.text, err = s.newText(); err != nil {
		return nil, err
	}
	s.search.results = make(chan searchResponse, 1)
	s.search.echo = a.UI.CmdHeight() != ui.CmdHeightToggle
	s.counts, s.lineTotals = newStatusCounts(), newLineCounts()
	s.runFailures = make(chan string, 8)
	s.indexWork = time.NewTimer(0)
	return s, nil
}

func (s *session) close() error {
	s.lineTotals.close()
	s.counts.close()
	s.search.close()
	s.indexWork.Stop()
	return s.text.Close()
}

func (s *session) newText() (*ui.Text, error) {
	text, err := ui.NewTextWithLimits(s.Document, s.metadataBudget, s.metadataQuota)
	if err != nil {
		return nil, err
	}
	text.SetWideAmbiguous(s.wideAmbiguous)
	text.SetUnprintable(s.unprintableChar)
	return text, nil
}

// clearPending drops a jump that is still waiting for the index.
func (s *session) clearPending() {
	s.pendingLine, s.hasPendingOffset, s.wantBottom = 0, false, false
}

// jumpTop places row at the jump target; the view never starts above row 0.
func (s *session) jumpTop(row int) int {
	return max(0, row-s.JumpTarget.Row(s.layout.Body.Height))
}

func (s *session) gutter() int {
	if !s.LineNumbers || s.layout.Body.Width <= max(3, s.gutterDigits)+1 {
		return 0
	}
	return max(3, s.gutterDigits) + 1
}

func (s *session) textWidth() int { return s.layout.Body.Width - s.gutter() }

// indexing reports whether the display index still lags the input.
func (s *session) indexing() bool { return s.text.NeedsUpdate(s.textWidth(), !s.reading) }

// revealColumn scrolls unwrapped rows so that offset is visible.
func (s *session) revealColumn(offset int64) {
	if s.Wrap {
		return
	}
	column := s.text.ColumnAt(offset)
	if column >= 0 && (column < s.left || column >= s.left+s.textWidth()) {
		s.left = 0
		if column >= s.textWidth() {
			s.left = column - s.textWidth()/2
		}
	}
}

// targetLine is the file line at the jump-target row. It is $line for run
// commands, so a search or :N jump sets it to the line it lands on, and the
// line a reload returns to.
func (s *session) targetLine() int {
	row := min(s.view.top+s.JumpTarget.Row(s.layout.Body.Height), s.text.RowCount()-1)
	return max(1, s.text.LogicalLineAt(s.text.Offset(max(0, row))))
}

// reload starts over with a fresh index when the watched file was truncated
// or replaced; byte positions and matches in the old content are meaningless,
// but the same line number is kept on screen (clamped to the new length).
func (s *session) reload() error {
	generation, reason := s.Document.Generation()
	if generation == s.loadedGeneration {
		return nil
	}
	keep := s.targetLine()
	replacement, err := s.newText()
	if err != nil {
		return err
	}
	if err := s.text.Close(); err != nil {
		replacement.Close()
		return err
	}
	s.text, s.loadedGeneration = replacement, generation
	s.search.stop()
	s.search.matched, s.search.repeats = false, nil
	label := s.display
	if label == "" {
		label = "input"
	}
	s.search.message = fmt.Sprintf("%s %s; reloaded", label, reason)
	s.clearPending()
	if !s.view.follow {
		s.pendingLine = keep // Following keeps the end in view instead.
	}
	s.view.top, s.left, s.gutterDigits, s.indexedWidth = 0, 0, 0, 0
	s.body.lines, s.body.styled, s.previousLength = nil, nil, -1
	s.dirty = true
	return nil
}

// lineKey names the current snapshot for the background line count.
func (s *session) lineKey() countKey {
	return countKey{base: s.Document.Start(), end: s.Document.Len(), generation: s.loadedGeneration}
}

// updateText extends the display index by one time slice and resolves
// pending jumps it has reached.
func (s *session) updateText() error {
	if err := s.reload(); err != nil {
		return err
	}
	anchor := s.text.Offset(s.view.top)
	s.text.SetWrap(s.Wrap)
	reflow := s.indexedWidth != s.textWidth() || s.indexedWrap != s.Wrap
	if reflow && s.indexedWidth != 0 && s.pendingLine == 0 && !s.hasPendingOffset && !s.wantBottom {
		s.pendingOffset, s.hasPendingOffset, s.pendingSearch = anchor, true, false
	}
	if s.indexing() {
		if err := s.text.UpdateFor(s.textWidth(), !s.reading, indexSlice); err != nil {
			return err
		}
		s.indexedWidth, s.indexedWrap = s.textWidth(), s.Wrap
		if reflow {
			s.view.top = s.text.RowAt(anchor)
		}
		s.dirty = true
	}
	if s.LineNumbers {
		lines := int64(s.text.LineCount())
		if total, ok := s.lineTotals.total(s.lineKey()); ok {
			lines = total // Size the gutter once instead of restarting the index as it grows.
		}
		s.gutterDigits = max(s.gutterDigits, len(strconv.FormatInt(lines, 10)))
	} else {
		s.gutterDigits = 0
	}
	pending := s.indexing()
	if s.pendingLine > 0 && (s.pendingLine <= s.text.LineCount() || !pending) {
		s.view.top = s.jumpTop(s.text.LineRow(s.pendingLine))
		s.pendingLine = 0
	}
	if s.hasPendingOffset && (s.pendingOffset < s.text.Indexed() || !pending) {
		s.view.top = s.text.RowAt(s.pendingOffset)
		if s.pendingSearch {
			s.view.top = s.jumpTop(s.view.top)
			s.revealColumn(s.pendingOffset)
		}
		s.hasPendingOffset = false
	}
	return nil
}

func (s *session) scheduleIndex() {
	if s.indexing() {
		s.indexWork.Reset(0)
	}
}

func (s *session) startSearch(origin int64, backward, include bool) {
	key := countKey{query: s.query(s.search.query), base: s.Document.Start(), end: s.Document.Len(), generation: s.loadedGeneration}
	chunks, indexed := s.counts.chunksFor(key)
	s.search.start(s.ctx, s.Document, key.query, chunks, indexed, origin, s.text.SearchAnchor(origin), backward, include)
}

func (s *session) jumpBottom() {
	s.clearPending()
	s.view.follow, s.view.bottom = false, false
	s.wantBottom = s.indexing()
	if s.wantBottom {
		s.bottomEnd = s.Document.Len()
	} else {
		s.view.apply(keybind.Bottom, s.text.RowCount(), s.layout.Body.Height)
	}
}

// searchDone shows a background search's result.
func (s *session) searchDone(response searchResponse) {
	if response.generation != s.search.generation {
		return
	}
	s.search.busy = false
	if response.result.Found && !response.available(s.Document.Start(), s.Document.Len()) {
		response.result.Found = false
		response.err = fmt.Errorf("match was discarded by rolling retention; repeat to search available history")
		s.search.matched = false
	}
	if response.err != nil {
		s.search.message = fmt.Sprintf("Search: %v", response.err)
	} else if !response.result.Found {
		s.search.message = "Pattern not found: " + s.search.query
		s.search.matched = false
	} else {
		s.search.match, s.search.matched = response.result.Offset, true
		s.clearPending()
		s.pendingOffset, s.hasPendingOffset, s.pendingSearch = s.search.match, true, true
		if s.search.match < s.text.Indexed() {
			s.view.top = s.jumpTop(s.text.RowAt(s.search.match))
			s.revealColumn(s.search.match)
			s.hasPendingOffset = false
		}
		s.view.follow, s.view.bottom = false, false
		s.search.message = s.search.echoQuery()
		if response.result.Wrapped && s.search.echo {
			s.search.message += "  [wrapped]"
		}
	}
	if s.search.warning != "" {
		s.search.message = strings.TrimSpace(s.search.message + "  [" + s.search.warning + "]")
	}
	if response.err == nil && response.result.Found && len(s.search.repeats) > 0 {
		backward := s.search.repeats[0]
		s.search.repeats = s.search.repeats[1:]
		s.startSearch(s.search.match, backward, false)
	} else if response.err != nil || !response.result.Found {
		s.search.repeats = nil
	}
	s.dirty = true
}

// tick runs once per frame: it flushes timed-out key sequences and repaints
// when anything changed.
func (s *session) tick(now time.Time) (quit bool, err error) {
	// A throttled recount may become due even when the viewport is idle.
	if !s.search.busy && s.search.query != "" && s.counts.recountDue(now, s.Document.Len()) {
		s.dirty = true
	}
	if s.search.prompt != nil && s.SearchKeys.NeedsTimeout() && !now.Before(s.deadline) {
		if s.applyPrompt(s.SearchKeys.Flush()) {
			return true, nil
		}
	}
	if s.Keys.NeedsTimeout() && !now.Before(s.deadline) {
		if s.apply(s.Keys.Flush()) {
			return true, nil
		}
	}
	length := s.Document.Len()
	generation, _ := s.Document.Generation()
	if s.dirty || length != s.previousLength || s.Document.Paused() != s.previousPaused || s.Document.WatchStatus() != s.previousWatchStatus || generation != s.loadedGeneration {
		if err := s.draw(length); err != nil {
			return false, err
		}
		s.dirty = false
	}
	return false, nil
}

func (s *session) draw(length int64) error {
	// Reload before the layout, so a reload message gets its footer row.
	if err := s.reload(); err != nil {
		return err
	}
	paused, discarded := s.Document.Paused(), s.Document.Start()
	watchStatus := s.Document.WatchStatus()
	s.UI.SetSearchVisible(s.search.prompt != nil || s.search.message != "" || paused || discarded > 0 || watchStatus != "")
	s.layout = s.UI.Layout(s.screen.Size())
	if err := s.updateText(); err != nil {
		return err
	}
	if s.wantBottom && (s.text.Indexed() >= s.bottomEnd || !s.indexing()) {
		// Resolve only with the final layout, just before painting. G targets
		// the input available when requested, not each successive indexed tail.
		rows := min(s.text.RowCount(), s.text.RowAt(max(0, s.bottomEnd-1))+1)
		s.view.top = max(0, rows-max(1, s.layout.Body.Height))
		s.view.bottom, s.wantBottom = false, false
	}
	anchor := s.text.Offset(s.view.top)
	s.view.reflow(s.text.RowAt(anchor), s.text.RowCount(), s.layout.Body.Height, length != s.previousLength || s.text.RowCount() != s.previousRows || s.text.Indexed() != s.previousIndexed)
	top := s.text.Offset(s.view.top)
	s.text.SetLeft(s.left)
	// While G is pending, only progress/status changes. Reuse an already
	// complete, immutable starting viewport instead of decoding it at 60 Hz.
	cached := s.wantBottom && s.body.layout == s.layout && s.body.left == s.left && s.body.offset == top && s.body.base == discarded && s.view.top+s.layout.Body.Height < s.previousRows
	lines, styled := s.body.lines, s.body.styled
	if !cached {
		var err error
		lines, err = s.text.Lines(s.view.top, s.layout.Body.Height)
		if err != nil {
			if errors.Is(err, pager.ErrEvicted) || errors.Is(err, pager.ErrTruncated) {
				return nil
			}
			return err
		}
		styled = s.text.Styled()
	}
	indexing := s.indexing()
	target := top
	if s.search.matched {
		target = s.search.match
	}
	key := countKey{query: s.query(s.search.query), base: discarded, end: s.Document.Len(), generation: s.loadedGeneration}
	state := s.status(top, target, key, indexing)
	bar := s.Statusline.Format(state, s.layout.Body.Width, s.wideAmbiguous)
	s.counts.visible = s.layout.StatusRow >= 0 && bar.Has(ui.StatusSearchResults)
	s.counts.update(s.ctx, s.Document, key, target, s.search.matched, s.search.busy || s.hasPendingOffset)
	frame := ui.Frame{
		Layout: s.layout, Lines: lines, Styled: styled,
		Status: bar.Text(), StatusBar: bar, StatusState: state,
		Search: s.footer(paused, discarded, watchStatus), WideAmbiguous: s.wideAmbiguous, HighlightStyle: s.SearchHighlight, UnprintableStyle: s.UnprintableStyle,
		Reading: s.reading, Indexing: indexing, SeekingEnd: s.wantBottom, Follow: s.view.follow, DocumentLength: s.Document.Len(),
	}
	s.decorate(&frame, key.query)
	s.showPrompt(&frame)
	if err := s.Renderer.Render(frame); err != nil {
		return err
	}
	s.body = bodyCache{lines: lines, styled: styled, layout: s.layout, offset: top, base: discarded, left: s.left}
	s.previousLength = length
	s.previousRows, s.previousIndexed = s.text.RowCount(), s.text.Indexed()
	s.previousPaused, s.previousWatchStatus = paused, watchStatus
	s.scheduleIndex()
	return nil
}

// status describes the view for the statusline; target is the position
// search results are ranked from.
func (s *session) status(top, target int64, key countKey, indexing bool) ui.StatusState {
	state := ui.StatusState{
		Mode: ui.ModeView, Filename: s.display, FilenamePath: s.displayPath, HasSearch: s.search.query != "",
		TopLine: s.text.LogicalLineAt(top), TotalLines: s.text.LineCount(), TotalLinesKnown: !s.reading && !indexing,
	}
	if !s.reading && indexing {
		// The display index is still decoding; a newline count finishes far sooner.
		s.lineTotals.request(s.ctx, s.Document, s.lineKey())
		if total, ok := s.lineTotals.total(s.lineKey()); ok {
			state.TotalLines, state.TotalLinesKnown = int(total), true
		}
	}
	state.SearchCurrent, state.SearchTotal, state.SearchCurrentKnown, state.SearchTotalKnown = s.counts.values(key, target, s.search.matched)
	switch {
	case s.help != nil:
		state.Mode = ui.ModeHelp
	case s.search.busy || s.search.prompt != nil && !s.search.prompt.command || s.pendingSearch && s.hasPendingOffset:
		state.Mode = ui.ModeSearch
	case s.view.follow:
		state.Mode = ui.ModeFollow
	}
	return state
}

// footer is the message row outside the prompt: the latest message, unless
// the watcher or storage has something to report.
func (s *session) footer(paused bool, discarded int64, watchStatus string) string {
	message := s.search.message
	if watchStatus != "" {
		message = watchStatus
	}
	if paused {
		message = "Storage paused: spool quota reached; :set spool-quota=BYTES to resume"
	}
	if discarded > 0 {
		message += fmt.Sprintf(" [rolling history: %d bytes discarded]", discarded)
	}
	return message
}

// decorate adds the gutter and highlights to the body, or replaces the body
// with the help screen while it is open.
func (s *session) decorate(frame *ui.Frame, query search.Query) {
	if s.help != nil {
		s.helpView.top = min(s.helpView.top, max(0, len(s.help)-s.layout.Body.Height))
		frame.Lines = s.help[s.helpView.top:min(len(s.help), s.helpView.top+s.layout.Body.Height)]
		frame.Styled = nil
		return
	}
	if width := s.gutter(); width > 0 {
		frame.GutterWidth, frame.GutterStyle = width, s.LineNumberStyle
		frame.Gutter = make([]string, len(frame.Lines))
		for row := range frame.Lines {
			if line, first := s.text.LineNumberAt(s.view.top + row); first {
				frame.Gutter[row] = strconv.Itoa(line)
			}
		}
	}
	if s.HexColors && s.truecolor {
		frame.HexColors = s.text.HexColors(frame.Lines, s.view.top)
	}
	if s.HighlightSearch {
		frame.Highlights = s.text.Highlights(frame.Lines, s.view.top, query)
	}
}

// showPrompt puts an open prompt on the message row.
func (s *session) showPrompt(frame *ui.Frame) {
	p := s.search.prompt
	if p == nil {
		return
	}
	prefix := "/"
	if p.backward {
		prefix = "?"
	}
	if p.command {
		prefix = ":"
	}
	frame.Search = prefix + string(p.text)
	frame.Ghost = p.ghost()
	if !p.command && len(p.text) > 0 {
		if err := s.query(string(p.text)).Validate(); err != nil {
			frame.Ghost = "  " + err.Error()
		}
	}
	frame.SearchActive = true
	frame.SearchCursor = 1 + p.cursor
}
