package keybind

import (
	"fmt"
	"strings"
)

type node struct {
	children map[Key]*node
	action   Action
}

// Matcher is a sequence trie with view-local pending input. Incomplete prefixes
// wait for more input; only ambiguous completed bindings need a timeout.
type Matcher struct {
	root     node
	bindings []Binding
	pending  []Key
	count    int
	insert   bool
}

// Command carries an optional numeric prefix. Count is zero when omitted.
type Command struct {
	Action Action
	Count  int
	Text   string
	// Remainder preserves raw keys after a mode-switching match, so the new
	// mode can interpret a mismatching suffix using its own bindings.
	Remainder []Key
}

func NewMatcher(bindings []Binding) (*Matcher, error) {
	m := &Matcher{root: node{children: make(map[Key]*node)}, bindings: bindings}
	for _, binding := range bindings {
		if binding.Action == "" || len(binding.Keys) == 0 {
			return nil, fmt.Errorf("keybindings must have an action and a nonempty sequence")
		}
		n := &m.root
		for _, key := range binding.Keys {
			if key == "" {
				return nil, fmt.Errorf("keybind %s contains an empty key", binding.Action)
			}
			if n.children[key] == nil {
				n.children[key] = &node{children: make(map[Key]*node)}
			}
			n = n.children[key]
		}
		if n.action != "" {
			return nil, fmt.Errorf("key sequence %v is bound to both %s and %s", binding.Keys, n.action, binding.Action)
		}
		n.action = binding.Action
	}
	return m, nil
}

// Bindings returns the effective bindings in definition order: defaults that
// were not replaced, followed by configured sequences.
func (m *Matcher) Bindings() []Binding { return m.bindings }

// Feed may return two actions when an ambiguous shorter binding is completed
// and the mismatching key also matches a binding at the root.
func (m *Matcher) Feed(key Key) []Command {
	m.pending = append(m.pending, key)
	return m.resolve(false)
}

func (m *Matcher) Cancel() { m.pending, m.count = nil, 0 }

// DrainLiteral settles prompt text before bracketed paste without executing a
// pending shortcut (in particular, an ambiguous accept/cancel binding).
func (m *Matcher) DrainLiteral() []Command {
	var commands []Command
	if m.insert {
		for _, key := range m.pending {
			if !strings.HasPrefix(string(key), "<") || key == "<" {
				commands = append(commands, Command{Action: Insert, Text: string(key)})
			}
		}
	}
	m.Cancel()
	return commands
}

// NeedsTimeout reports whether pending input includes a completed binding that
// could fire instead of a longer sequence. A bare "g" in "gg" has no action to
// fall back to, so it should wait rather than time out and disappear.
func (m *Matcher) NeedsTimeout() bool {
	n := &m.root
	for _, key := range m.pending {
		n = n.children[key]
		if n.action != "" {
			return true
		}
	}
	return false
}

// Flush resolves an ambiguous timeout. Pure prefixes remain pending until a
// continuation, a mismatch, or an explicit cancellation arrives.
func (m *Matcher) Flush() []Command {
	if !m.NeedsTimeout() {
		return nil
	}
	return m.resolve(true)
}

func (m *Matcher) resolve(expired bool) []Command {
	var actions []Command
	for len(m.pending) > 0 {
		// Explicit digit bindings take precedence over starting a count. Once
		// started, a count consumes all following digits, saturating on overflow.
		key := m.pending[0]
		if !m.insert && len(key) == 1 && key[0] >= '0' && key[0] <= '9' &&
			(m.count > 0 || (key != "0" && m.root.children[key] == nil)) {
			digit := int(key[0] - '0')
			limit := int(^uint(0) >> 1)
			if m.count > (limit-digit)/10 {
				m.count = limit
			} else {
				m.count = m.count*10 + digit
			}
			m.pending = m.pending[1:]
			continue
		}
		n := &m.root
		matched, lastMatch := 0, 0
		var action Action
		for matched < len(m.pending) && n.children[m.pending[matched]] != nil {
			n = n.children[m.pending[matched]]
			matched++
			if n.action != "" {
				action, lastMatch = n.action, matched
			}
		}
		if matched == len(m.pending) && len(n.children) > 0 && !expired {
			break
		}
		if action != "" {
			command := Command{Action: action, Count: m.count}
			if action == SearchForward || action == SearchBackward || action == CommandInput || (m.insert && (action == SearchCancel || action == Accept)) {
				command.Remainder = append([]Key(nil), m.pending[lastMatch:]...)
			}
			actions = append(actions, command)
			m.count = 0
			m.pending = m.pending[lastMatch:]
		} else if matched == len(m.pending) {
			m.Cancel()
		} else {
			// Discard an incomplete prefix, but retry the mismatching key.
			m.count = 0
			if m.insert {
				for _, key := range m.pending[:max(1, matched)] {
					if !strings.HasPrefix(string(key), "<") || key == "<" {
						actions = append(actions, Command{Action: Insert, Text: string(key)})
					}
				}
			}
			m.pending = m.pending[max(1, matched):]
		}
	}
	return actions
}
