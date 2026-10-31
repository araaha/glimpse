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
	FilterInput        Action = "filter"
	SetMark            Action = "set-mark"
	JumpToMark         Action = "jump-to-mark"
	ToggleWrap         Action = "toggle-wrap"
	ScrollLeft         Action = "scroll-left"
	ScrollRight        Action = "scroll-right"
	ToggleLineNumbers  Action = "toggle-line-numbers"
	ToggleHexColors    Action = "toggle-hex-colors"
	Edit               Action = "edit"
	Help               Action = "help"
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
	Complete           Action = "search-complete"
	CompletePrevious   Action = "search-complete-previous"
)

// Arguments are the actions that take the key typed after their sequence,
// such as the name of a mark, as Command.Text.
var Arguments = map[Action]bool{SetMark: true, JumpToMark: true}

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
		{FilterInput, []Key{"&"}},
		{SetMark, []Key{"m"}},
		{JumpToMark, []Key{"'"}},
		{ToggleWrap, []Key{"w"}},
		{ScrollLeft, []Key{"h"}},
		{ScrollLeft, []Key{"<Left>"}},
		{ScrollRight, []Key{"l"}},
		{ScrollRight, []Key{"<Right>"}},
		{ToggleLineNumbers, []Key{"#"}},
		{ToggleHexColors, []Key{"R"}},
		{Edit, []Key{"v"}},
		{Help, []Key{"g", "?"}},
	}
}

// Compile replaces the default sequences for each configured action. A TOML
// array is one sequence: top = ["g", "g"]. An empty array disables that action.
// Explicit sequences also replace matching default bindings for other actions.
// Extra bindings, such as configured run commands, are added last and may not
// share a sequence with any other binding.
func Compile(overrides map[string][]string, extra ...Binding) (*Matcher, error) {
	selected, err := modeOverrides(Defaults(), overrides)
	if err != nil {
		return nil, err
	}
	m, err := compile(Defaults(), selected)
	if err != nil || len(extra) == 0 {
		return m, err
	}
	return NewMatcher(append(slices.Clip(m.Bindings()), extra...))
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
		{Complete, []Key{"<Tab>"}}, {CompletePrevious, []Key{"<S-Tab>"}},
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
		case "Up", "Down", "Left", "Right", "Home", "End", "PageUp", "PageDown", "Enter", "Tab", "S-Tab", "Backspace", "Delete", "Esc":
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

// Descriptions explain each action for the help screen.
var Descriptions = map[Action]string{
	Quit:               "quit",
	Cancel:             "cancel a pending key sequence or search",
	Top:                "go to the first line",
	Bottom:             "go to the last line, or line N with a count",
	Up:                 "scroll up one row",
	Down:               "scroll down one row",
	PageUp:             "scroll up one screen",
	PageDown:           "scroll down one screen",
	HalfPageUp:         "scroll up half a screen",
	HalfPageDown:       "scroll down half a screen",
	Follow:             "toggle following new input",
	ToggleStatus:       "toggle the statusline (cmdheight -1/-2)",
	SearchForward:      "search forward",
	SearchBackward:     "search backward",
	SearchNext:         "repeat the search",
	SearchPrevious:     "repeat the search in the opposite direction",
	CommandInput:       "enter a : command",
	FilterInput:        "show only lines matching a pattern; empty shows every line",
	SetMark:            "mark the line at the jump target: m then a letter",
	JumpToMark:         "go to a marked line: ' then a letter, or ' for the previous position",
	ToggleWrap:         "toggle line wrapping",
	ScrollLeft:         "scroll unwrapped lines left",
	ScrollRight:        "scroll unwrapped lines right",
	ToggleLineNumbers:  "toggle line numbers",
	ToggleHexColors:    "toggle coloring #RRGGBB codes",
	Edit:               "edit the file at $line in $VISUAL or $EDITOR",
	Help:               "toggle this help",
	SearchCancel:       "cancel the prompt",
	Accept:             "run the search or command",
	Left:               "move left",
	Right:              "move right",
	Start:              "move to the start",
	End:                "move to the end",
	WordLeft:           "move one word left",
	WordRight:          "move one word right",
	Backspace:          "delete the previous character",
	Delete:             "delete the next character",
	DeleteWordBackward: "delete the previous word",
	DeleteWordForward:  "delete the next word",
	ClearBefore:        "delete to the start",
	ClearAfter:         "delete to the end",
	HistoryPrevious:    "previous history entry",
	HistoryNext:        "next history entry",
	Complete:           "complete a : command, or cycle to the next completion",
	CompletePrevious:   "cycle to the previous : command completion",
}

// Actions lists view and search-prompt actions in their default order.
func Actions() (view, prompt []Action) {
	unique := func(bindings []Binding) []Action {
		var actions []Action
		for _, binding := range bindings {
			if !slices.Contains(actions, binding.Action) {
				actions = append(actions, binding.Action)
			}
		}
		return actions
	}
	return unique(Defaults()), unique(searchDefaults())
}

// Format writes a sequence the way it is configured, joining its keys.
func Format(keys []Key) string {
	var text strings.Builder
	for _, key := range keys {
		if key == " " {
			key = "<Space>"
		}
		text.WriteString(string(key))
	}
	return text.String()
}
