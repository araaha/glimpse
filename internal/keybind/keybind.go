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
	Quit         Action = "quit"
	Cancel       Action = "cancel"
	Top          Action = "top"
	Bottom       Action = "bottom"
	Up           Action = "up"
	Down         Action = "down"
	PageUp       Action = "page-up"
	PageDown     Action = "page-down"
	HalfPageUp   Action = "half-page-up"
	HalfPageDown Action = "half-page-down"
	Follow       Action = "follow"
	ToggleStatus Action = "toggle-status"
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
	}
}

// Compile replaces the default sequences for each configured action. A TOML
// array is one sequence: top = ["g", "g"]. An empty array disables that action.
// Explicit sequences also replace matching default bindings for other actions.
func Compile(overrides map[string][]string) (*Matcher, error) {
	defaults := Defaults()
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
	if token == "<Space>" {
		return " ", nil
	}
	switch token {
	case "<Up>", "<Down>", "<Left>", "<Right>", "<Home>", "<End>",
		"<PageUp>", "<PageDown>", "<Enter>", "<Tab>", "<Backspace>", "<Esc>":
		return Key(token), nil
	}
	if len(token) == 5 && strings.HasPrefix(token, "<C-") && token[4] == '>' {
		c := token[3]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c >= 'a' && c <= 'z' {
			return Key("<C-" + string(c) + ">"), nil
		}
	}
	return "", fmt.Errorf("invalid key %q (use separate array entries for a sequence)", token)
}
