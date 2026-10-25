package keybind

import "fmt"

type node struct {
	children map[Key]*node
	action   Action
}

// Matcher is a sequence trie with view-local pending input. Incomplete prefixes
// wait for more input; only ambiguous completed bindings need a timeout.
type Matcher struct {
	root    node
	pending []Key
}

func NewMatcher(bindings []Binding) (*Matcher, error) {
	m := &Matcher{root: node{children: make(map[Key]*node)}}
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

// Feed may return two actions when an ambiguous shorter binding is completed
// and the mismatching key also matches a binding at the root.
func (m *Matcher) Feed(key Key) []Action {
	m.pending = append(m.pending, key)
	return m.resolve(false)
}

func (m *Matcher) Pending() bool { return len(m.pending) != 0 }
func (m *Matcher) Cancel()       { m.pending = nil }

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
func (m *Matcher) Flush() []Action {
	if !m.NeedsTimeout() {
		return nil
	}
	return m.resolve(true)
}

func (m *Matcher) resolve(expired bool) []Action {
	var actions []Action
	for len(m.pending) > 0 {
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
			actions = append(actions, action)
			m.pending = m.pending[lastMatch:]
		} else if matched == len(m.pending) {
			m.Cancel()
		} else {
			// Discard an incomplete prefix, but retry the mismatching key.
			m.pending = m.pending[max(1, matched):]
		}
	}
	return actions
}
