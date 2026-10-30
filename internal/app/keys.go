package app

import (
	"fmt"
	"time"

	"glimpse/internal/keybind"
	"glimpse/internal/ui"

	"github.com/gdamore/tcell/v3"
)

// handle processes one terminal event; quit reports that glimpse should exit.
func (s *session) handle(event tcell.Event) (quit bool, err error) {
	switch ev := event.(type) {
	case *tcell.EventPaste:
		if ev.Start() {
			s.Keys.Cancel()
			if s.search.prompt != nil && s.applyPrompt(s.SearchKeys.DrainLiteral()) {
				return true, nil
			}
		}
		s.pasting = ev.Start()
	case *tcell.EventResize:
		s.screen.Sync()
		s.dirty = true
	case *tcell.EventError:
		return false, fmt.Errorf("terminal: %w", ev)
	case *tcell.EventKey:
		if s.pasting {
			if s.search.prompt != nil && ev.Pressed() && ev.Key() == tcell.KeyRune {
				s.search.prompt.edit(keybind.Command{Action: keybind.Insert, Text: ev.Str()})
				s.preview()
				s.dirty = true
			}
			return false, nil
		}
		keys := ui.KeysFromEvent(ev)
		if len(keys) == 0 {
			return false, nil
		}
		if s.dispatchKeys(keys) {
			return true, nil
		}
		s.deadline = time.Now().Add(sequenceTimeout)
	}
	return false, nil
}

// dispatchKeys feeds keys to the prompt while it is open, else to the view.
func (s *session) dispatchKeys(keys []keybind.Key) (quit bool) {
	var batches []*prompt
	defer func() {
		for _, p := range batches {
			p.batching = false
			p.cursor = p.align(p.cursor, true)
		}
	}()
	for _, key := range keys {
		if s.search.prompt != nil {
			if len(keys) > 1 && !s.search.prompt.batching {
				s.search.prompt.batching = true
				batches = append(batches, s.search.prompt)
			}
			if s.applyPrompt(s.SearchKeys.Feed(key)) {
				return true
			}
		} else if s.apply(s.Keys.Feed(key)) {
			return true
		}
	}
	return false
}

// apply runs view commands and reports whether one of them quits.
func (s *session) apply(commands []keybind.Command) (quit bool) {
	if s.help != nil {
		s.applyHelp(commands)
		return false
	}
	// Discard unresolved actions before cancellation, not commands retried
	// afterward. The trie has already consumed the canceled prefix; any
	// pending input now belongs to a new command and must be preserved.
	for i := len(commands) - 1; i >= 0; i-- {
		if commands[i].Action == keybind.Cancel {
			s.clearPending()
			s.search.stop()
			s.search.message = ""
			s.dirty = true
			commands = commands[i+1:]
			break
		}
	}
	for _, command := range commands {
		switch command.Action {
		case keybind.Quit:
			return true
		case keybind.SearchForward, keybind.SearchBackward, keybind.CommandInput:
			// The prompt takes the rest of the key sequence.
			return s.openPrompt(command)
		case keybind.SearchNext, keybind.SearchPrevious:
			s.repeatSearch(command.Action == keybind.SearchPrevious)
		case keybind.Help:
			s.help, s.helpView = helpLines(s.Keys, s.SearchKeys, s.runs), viewState{}
		case keybind.ScrollLeft, keybind.ScrollRight:
			s.scroll(command.Action)
		case keybind.Edit:
			s.runCommand(command.Action)
		default:
			if _, ok := s.runs[command.Action]; ok {
				s.runCommand(command.Action)
			} else {
				s.move(command)
			}
		}
		s.dirty = true
	}
	return false
}

// applyHelp scrolls the help screen, which takes over navigation. Closing it
// drops the rest of the batch rather than acting on the document behind it.
func (s *session) applyHelp(commands []keybind.Command) {
	s.dirty = true
	for _, command := range commands {
		switch command.Action {
		case keybind.Quit, keybind.Cancel, keybind.Help:
			s.help = nil
			return
		case keybind.Top, keybind.Bottom, keybind.Up, keybind.Down, keybind.PageUp, keybind.PageDown, keybind.HalfPageUp, keybind.HalfPageDown:
			s.helpView.apply(command.Action, len(s.help), s.layout.Body.Height)
		}
	}
}

// openPrompt opens the search or command prompt and feeds it the keys that
// followed in the same sequence.
func (s *session) openPrompt(command keybind.Command) (quit bool) {
	s.clearPending()
	s.search.begin(command.Action == keybind.SearchBackward)
	if command.Action == keybind.CommandInput {
		s.search.prompt = newPrompt(false, s.search.commandHistory.Entries)
		s.search.prompt.command = true
	}
	// Opening a prompt keeps following; only a match or a jump moves the view.
	s.search.prompt.origin, s.search.prompt.follow = s.text.Offset(s.view.top), s.view.follow
	s.Keys.Cancel()
	s.SearchKeys.Cancel()
	s.dirty = true
	for _, key := range command.Remainder {
		if s.applyPrompt(s.SearchKeys.Feed(key)) {
			return true
		}
	}
	return false
}

// repeatSearch looks for the next match, or the previous one with reverse.
func (s *session) repeatSearch(reverse bool) {
	s.clearPending()
	if s.search.query == "" {
		s.search.message = "No previous search"
		return
	}
	// Without a selected match, repeat from the jump target row.
	origin, include := s.text.Offset(s.view.top+s.JumpTarget.Row(s.layout.Body.Height)), true
	if s.search.matched {
		origin, include = s.search.match, false
	}
	backward := s.search.backward != reverse
	s.view.follow, s.view.bottom = false, false
	if s.search.busy {
		s.search.repeats = append(s.search.repeats, backward)
	} else {
		s.startSearch(origin, backward, include)
	}
}

// scroll moves unwrapped rows by half a screen, as in less. Scrolling right
// stops once no visible row continues past the edge.
func (s *session) scroll(action keybind.Action) {
	if s.Wrap {
		return
	}
	step := max(1, s.textWidth()/2)
	if action == keybind.ScrollLeft {
		s.left = max(0, s.left-step)
	} else if s.text.Overflow() {
		s.left += step
	}
}

// runCommand runs a [[run]] binding, or the built-in editor for Edit.
func (s *session) runCommand(action keybind.Action) {
	run, ok := s.runs[action]
	if !ok {
		run = RunCommand{Command: editCommand, label: "edit"}
	}
	file := commandFile(s.name)
	s.search.message = ""
	if !ok && file == "" {
		s.search.message = "No file to edit"
		return
	}
	cmd := shellCommand(run.Command, s.targetLine(), file)
	if run.Background {
		runBackground(cmd, run.label, s.runFailures)
		return
	}
	if err := runForeground(s.screen, cmd); err != nil {
		s.search.message = commandFailure(run.label, err)
	}
	// An editor may have saved by renaming a new file over the old one.
	s.Document.RecheckName()
}

// move applies a toggle or a movement, either of which ends the current search.
func (s *session) move(command keybind.Command) {
	switch command.Action {
	case keybind.ToggleStatus:
		s.UI.ToggleStatusline()
	case keybind.ToggleLineNumbers:
		s.LineNumbers = !s.LineNumbers
	case keybind.ToggleHexColors:
		s.HexColors = !s.HexColors
	case keybind.ToggleWrap:
		s.Wrap, s.left = !s.Wrap, 0
	case keybind.Bottom:
		if command.Count > 0 {
			s.clearPending()
			s.pendingLine = command.Count
			s.view.follow, s.view.bottom = false, false
		} else {
			s.jumpBottom()
		}
	default:
		s.clearPending()
		s.view.apply(command.Action, s.text.RowCount(), s.layout.Body.Height)
	}
	s.search.stop()
	s.search.matched, s.search.message = false, ""
	if command.Action == keybind.ToggleHexColors && s.HexColors && !s.truecolor {
		s.search.message = hexColorsUnsupported
	}
}

// applyPrompt runs prompt commands and reports whether one of them quits.
func (s *session) applyPrompt(commands []keybind.Command) (quit bool) {
	for i := len(commands) - 1; i >= 0; i-- {
		if commands[i].Action == keybind.SearchCancel {
			commands = commands[i:]
			break
		}
	}
	for _, command := range commands {
		if s.search.prompt == nil {
			break
		}
		s.dirty = true
		switch command.Action {
		case keybind.SearchCancel:
			s.cancelPrompt()
		case keybind.Accept:
			if s.accept() {
				return true
			}
		default:
			before := string(s.search.prompt.text)
			s.search.prompt.edit(command)
			if string(s.search.prompt.text) != before {
				s.preview()
			}
			continue
		}
		// The prompt has closed; the rest of the key sequence goes to the view.
		for _, key := range command.Remainder {
			if s.apply(s.Keys.Feed(key)) {
				return true
			}
		}
		return false
	}
	return false
}

// cancelPrompt closes the prompt and returns to the view and search it opened on.
func (s *session) cancelPrompt() {
	s.clearPending()
	s.search.stop()
	s.returnToOrigin(s.search.prompt)
	s.search.restore()
	s.search.prompt, s.search.message = nil, ""
	s.SearchKeys.Cancel()
}

// accept closes the prompt and runs its search or command.
func (s *session) accept() (quit bool) {
	p := s.search.prompt
	query := string(p.text)
	s.search.prompt = nil
	s.SearchKeys.Cancel()
	if p.command {
		return s.execute(query)
	}
	if query == "" {
		query = s.search.original.query
	}
	if query == "" {
		s.search.message = "No previous search"
		return false
	}
	if err := s.search.history.Add(query); err != nil {
		s.search.warning = fmt.Sprintf("history: %v", err)
	}
	if err := s.query(query).Validate(); err != nil {
		s.search.restore()
		s.search.message = err.Error()
		return false
	}
	s.search.query, s.search.backward, s.search.matched = query, p.backward, false
	s.startSearch(p.origin, p.backward, true)
	return false
}

// execute runs a : command.
func (s *session) execute(input string) (quit bool) {
	result, err := s.command(input)
	if err != nil {
		s.search.message = err.Error()
		return false
	}
	if result.quit {
		return true
	}
	s.search.message = result.message
	switch {
	case result.line > 0:
		s.search.matched, s.view.follow = false, false
		s.clearPending()
		s.pendingLine = result.line
	case result.action == keybind.Bottom:
		s.search.matched = false
		s.jumpBottom()
	case result.action != "":
		s.search.matched = false
		s.clearPending()
		s.view.apply(result.action, s.text.RowCount(), s.layout.Body.Height)
	}
	if input != "" {
		if err := s.search.commandHistory.Add(input); err != nil {
			s.search.message += fmt.Sprintf(" [history: %v]", err)
		}
	}
	return false
}

// preview searches as the prompt is typed, with incremental search.
func (s *session) preview() {
	p := s.search.prompt
	if p == nil || p.command || !s.IncrementalSearch {
		return
	}
	s.clearPending()
	s.search.stop()
	query := string(p.text)
	if query == "" {
		s.search.restore()
		s.search.message = ""
		s.returnToOrigin(p)
		return
	}
	if s.query(query).Validate() != nil {
		// Keep the view and the previous matches until the pattern is valid.
		s.search.restore()
		s.search.message = ""
		return
	}
	s.search.query, s.search.backward, s.search.matched = query, p.backward, false
	s.startSearch(p.origin, p.backward, true)
}

// returnToOrigin undoes an incremental preview: back to the row the prompt
// opened on, or to the end if the view was following.
func (s *session) returnToOrigin(p *prompt) {
	s.view.follow = p.follow
	if p.follow {
		s.view.bottom = true
		return
	}
	s.view.top = s.text.RowAt(p.origin)
}
