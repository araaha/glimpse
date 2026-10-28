// Package keybind matches normalized key events against configurable sequences.
// It has no dependency on the terminal backend or document storage.
package keybind

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

type Key string
type Action string

const (
	Quit               Action = "quit"
	Cancel             Action = "cancel"
	Top                Action = "top"
	Bottom             Action = "bottom"
	Up                 Action = "up"
	Down               Action = "down"
	PageUp             Action = "page-up"
	PageDown           Action = "page-down"
	HalfPageUp         Action = "half-page-up"
	HalfPageDown       Action = "half-page-down"
	Follow             Action = "follow"
	ToggleStatus       Action = "toggle-status"
	SearchForward      Action = "search-forward"
	SearchBackward     Action = "search-backward"
	SearchNext         Action = "search-next"
	SearchPrevious     Action = "search-previous"
	CommandInput       Action = "command"
	ToggleWrap         Action = "toggle-wrap"
	ScrollLeft         Action = "scroll-left"
	ScrollRight        Action = "scroll-right"
	ToggleLineNumbers  Action = "toggle-line-numbers"
	Insert             Action = "insert"
	SearchCancel       Action = "search-cancel"
	Accept             Action = "search-accept"
	Left               Action = "search-left"
	Right              Action = "search-right"
	Start              Action = "search-start"
	End                Action = "search-end"
	WordLeft           Action = "search-word-left"
	WordRight          Action = "search-word-right"
	Backspace          Action = "search-backspace"
	Delete             Action = "search-delete"
	DeleteWordBackward Action = "search-delete-word-backward"
	DeleteWordForward  Action = "search-delete-word-forward"
	ClearBefore        Action = "search-clear-before"
	ClearAfter         Action = "search-clear-after"
	HistoryPrevious    Action = "search-history-previous"
	HistoryNext        Action = "search-history-next"
)

type Binding struct {
	Action Action
	Keys   []Key
}

// Defaults returns independent bindings, including aliases for common keys.
func Defaults() []Binding {
	return []Binding{
		{Quit, []Key{"q"}},
		{Quit, []Key{"z", "q"}},
		{Quit, []Key{"<C-c>"}},
		{Cancel, []Key{"<Esc>"}},
		{Top, []Key{"g", "g"}},
		{Top, []Key{"<Home>"}},
		{Bottom, []Key{"G"}},
		{Bottom, []Key{"<End>"}},
		{Up, []Key{"k"}},
		{Up, []Key{"<Up>"}},
		{Down, []Key{"j"}},
		{Down, []Key{"<Down>"}},
		{PageUp, []Key{"<C-b>"}},
		{PageUp, []Key{"<PageUp>"}},
		{PageDown, []Key{"<C-f>"}},
		{PageDown, []Key{"<PageDown>"}},
		{PageDown, []Key{" "}},
		{HalfPageUp, []Key{"<C-u>"}},
		{HalfPageDown, []Key{"<C-d>"}},
		{Follow, []Key{"F"}},
		{ToggleStatus, []Key{"="}},
		{SearchForward, []Key{"/"}},
		{SearchBackward, []Key{"?"}},
		{SearchNext, []Key{"n"}},
		{SearchPrevious, []Key{"N"}},
		{CommandInput, []Key{":"}},
		{ToggleWrap, []Key{"w"}},
		{ScrollLeft, []Key{"h"}},
		{ScrollLeft, []Key{"<Left>"}},
		{ScrollRight, []Key{"l"}},
		{ScrollRight, []Key{"<Right>"}},
		{ToggleLineNumbers, []Key{"#"}},
	}
}

// Compile replaces the default sequences for each configured action. A TOML
// array is one sequence: top = ["g", "g"]. An empty array disables that action.
// Explicit sequences also replace matching default bindings for other actions.
func Compile(overrides map[string][]string) (*Matcher, error) {
	selected, err := modeOverrides(Defaults(), overrides)
	if err != nil {
		return nil, err
	}
	return compile(Defaults(), selected)
}

// CompileSearch builds a separate key map for the search prompt. Unbound
// printable keys insert text; numeric prefixes are disabled in this mode.
func CompileSearch(overrides map[string][]string) (*Matcher, error) {
	selected, err := modeOverrides(searchDefaults(), overrides)
	if err != nil {
		return nil, err
	}
	m, err := compile(searchDefaults(), selected)
	if err == nil {
		m.insert = true
	}
	return m, err
}

func searchDefaults() []Binding {
	return []Binding{
		{Accept, []Key{"<Enter>"}}, {Accept, []Key{"<C-j>"}},
		{SearchCancel, []Key{"<Esc>"}}, {SearchCancel, []Key{"<C-c>"}}, {SearchCancel, []Key{"<C-g>"}},
		{Left, []Key{"<Left>"}}, {Left, []Key{"<C-b>"}},
		{Right, []Key{"<Right>"}}, {Right, []Key{"<C-f>"}},
		{Start, []Key{"<Home>"}}, {Start, []Key{"<C-a>"}},
		{End, []Key{"<End>"}}, {End, []Key{"<C-e>"}},
		{WordLeft, []Key{"<M-b>"}}, {WordLeft, []Key{"<C-Left>"}},
		{WordRight, []Key{"<M-f>"}}, {WordRight, []Key{"<C-Right>"}},
		{Backspace, []Key{"<Backspace>"}}, {Backspace, []Key{"<C-h>"}},
		{Delete, []Key{"<Delete>"}}, {Delete, []Key{"<C-d>"}},
		{DeleteWordBackward, []Key{"<C-w>"}}, {DeleteWordBackward, []Key{"<M-Backspace>"}},
		{DeleteWordForward, []Key{"<M-d>"}},
		{ClearBefore, []Key{"<C-u>"}}, {ClearAfter, []Key{"<C-k>"}},
		{HistoryPrevious, []Key{"<Up>"}}, {HistoryPrevious, []Key{"<C-p>"}},
		{HistoryNext, []Key{"<Down>"}}, {HistoryNext, []Key{"<C-n>"}},
	}
}

// Both mode matchers consume one public [keybinds] table. Bindings only collide
// within their own mode; view actions cannot steal prompt shortcuts or vice versa.
func modeOverrides(defaults []Binding, overrides map[string][]string) (map[string][]string, error) {
	known, active := make(map[string]bool), make(map[string]bool)
	for _, binding := range append(Defaults(), searchDefaults()...) {
		known[string(binding.Action)] = true
	}
	for _, binding := range defaults {
		active[string(binding.Action)] = true
	}
	selected := make(map[string][]string)
	for action, sequence := range overrides {
		if !known[action] {
			return nil, fmt.Errorf("unknown keybind action %q", action)
		}
		if active[action] {
			selected[action] = sequence
		}
	}
	return selected, nil
}

func compile(defaults []Binding, overrides map[string][]string) (*Matcher, error) {
	known := make(map[Action]bool)
	for _, binding := range defaults {
		known[binding.Action] = true
	}
	var configured []Binding
	var names []string
	for name := range overrides {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		if !known[Action(name)] {
			return nil, fmt.Errorf("unknown keybind action %q", name)
		}
		keys := make([]Key, 0, len(overrides[name]))
		for _, token := range overrides[name] {
			key, err := ParseKey(token)
			if err != nil {
				return nil, fmt.Errorf("keybind %s: %w", name, err)
			}
			keys = append(keys, key)
		}
		if len(keys) > 0 {
			configured = append(configured, Binding{Action(name), keys})
		}
	}
	var bindings []Binding
	for _, binding := range defaults {
		if _, replaced := overrides[string(binding.Action)]; replaced {
			continue
		}
		claimed := false
		for _, explicit := range configured {
			if slices.Equal(binding.Keys, explicit.Keys) {
				claimed = true
				break
			}
		}
		if !claimed {
			bindings = append(bindings, binding)
		}
	}
	bindings = append(bindings, configured...)
	return NewMatcher(bindings)
}

// ParseKey accepts one literal character or a named key in Vim-style notation.
func ParseKey(token string) (Key, error) {
	if utf8.ValidString(token) && utf8.RuneCountInString(token) == 1 {
		r, _ := utf8.DecodeRuneInString(token)
		if r >= ' ' && r != 0x7f {
			return Key(token), nil
		}
	}
	if strings.HasPrefix(token, "<") && strings.HasSuffix(token, ">") {
		base := token[1 : len(token)-1]
		ctrl, meta := false, false
		for strings.HasPrefix(base, "C-") || strings.HasPrefix(base, "M-") {
			if base[:2] == "C-" {
				if ctrl {
					break
				}
				ctrl = true
			} else {
				if meta {
					break
				}
				meta = true
			}
			base = base[2:]
		}
		if base == "Space" {
			base = " "
		}
		named := false
		switch base {
		case "Up", "Down", "Left", "Right", "Home", "End", "PageUp", "PageDown", "Enter", "Tab", "Backspace", "Delete", "Esc":
			named = true
		}
		literal := utf8.ValidString(base) && utf8.RuneCountInString(base) == 1
		if literal {
			r, _ := utf8.DecodeRuneInString(base)
			literal = r >= ' ' && r != 0x7f
		}
		if named || (literal && (ctrl || meta || base == " ")) {
			modifier := ""
			if ctrl {
				modifier += "C-"
				if literal {
					base = strings.ToLower(base)
				}
			}
			if meta {
				modifier += "M-"
			}
			if modifier == "" && base == " " {
				return " ", nil
			}
			return Key("<" + modifier + base + ">"), nil
		}
	}
	return "", fmt.Errorf("invalid key %q (use separate array entries for a sequence)", token)
}
