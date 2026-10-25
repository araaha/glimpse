package keybind

import (
	"reflect"
	"strings"
	"testing"
)

func TestDefaultSequences(t *testing.T) {
	tests := []struct {
		keys []Key
		want Action
	}{
		{[]Key{"g", "g"}, Top},
		{[]Key{"G"}, Bottom},
		{[]Key{"z", "q"}, Quit},
		{[]Key{"q"}, Quit},
		{[]Key{"<C-c>"}, Quit},
		{[]Key{"<Esc>"}, Cancel},
		{[]Key{"j"}, Down},
		{[]Key{"k"}, Up},
		{[]Key{"="}, ToggleStatus},
		{[]Key{"<C-f>"}, PageDown},
		{[]Key{"<C-b>"}, PageUp},
		{[]Key{"<C-d>"}, HalfPageDown},
		{[]Key{"<C-u>"}, HalfPageUp},
	}
	for _, tt := range tests {
		t.Run(strings.Join(keyStrings(tt.keys), " "), func(t *testing.T) {
			matcher, err := Compile(nil)
			if err != nil {
				t.Fatal(err)
			}
			for i, key := range tt.keys {
				got := matcher.Feed(key)
				if i < len(tt.keys)-1 {
					if len(got) != 0 || !matcher.Pending() {
						t.Fatalf("prefix %v: actions = %v, pending = %v", tt.keys[:i+1], got, matcher.Pending())
					}
				} else if !reflect.DeepEqual(got, []Command{{Action: tt.want}}) || matcher.Pending() {
					t.Fatalf("sequence %v: actions = %v, pending = %v", tt.keys, got, matcher.Pending())
				}
			}
		})
	}
}

func keyStrings(keys []Key) []string {
	result := make([]string, len(keys))
	for i, key := range keys {
		result[i] = string(key)
	}
	return result
}

func TestMismatchRetriesKeyAtRoot(t *testing.T) {
	m, _ := Compile(nil)
	m.Feed("g")
	if got := m.Feed("j"); !reflect.DeepEqual(got, []Command{{Action: Down}}) {
		t.Fatalf("gj = %v, want down", got)
	}
	if m.Pending() {
		t.Fatal("mismatch left pending input")
	}
}

func TestTimeoutAndCancellation(t *testing.T) {
	m, _ := Compile(nil)
	m.Feed("g")
	if m.NeedsTimeout() {
		t.Fatal("an incomplete g prefix should not have a timeout")
	}
	for range 5 {
		if got := m.Flush(); len(got) != 0 || !m.Pending() {
			t.Fatalf("timeout discarded incomplete sequence: actions = %v, pending = %v", got, m.Pending())
		}
	}
	if got := m.Feed("g"); !reflect.DeepEqual(got, []Command{{Action: Top}}) || m.NeedsTimeout() {
		t.Fatalf("gg after timeout checks = %v, needs timeout = %v", got, m.NeedsTimeout())
	}

	m.Feed("g")
	m.Cancel()
	if got := m.Feed("g"); len(got) != 0 || !m.Pending() {
		t.Fatal("cancelled prefix was reused")
	}
}

func TestAmbiguousPrefixes(t *testing.T) {
	bindings := []Binding{
		{Top, []Key{"g"}},
		{Bottom, []Key{"g", "z", "q"}},
		{Quit, []Key{"q"}},
	}
	m, err := NewMatcher(bindings)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Feed("g"); len(got) != 0 {
		t.Fatalf("ambiguous g fired immediately: %v", got)
	}
	if !m.NeedsTimeout() {
		t.Fatal("an ambiguous completed binding should have a timeout")
	}
	if got := m.Flush(); !reflect.DeepEqual(got, []Command{{Action: Top}}) {
		t.Fatalf("timeout = %v, want top", got)
	}
	if m.NeedsTimeout() {
		t.Fatal("completed timeout left another timeout pending")
	}
	m.Feed("g")
	m.Feed("z")
	if !m.NeedsTimeout() {
		t.Fatal("a longer incomplete prefix lost its shorter completed fallback")
	}
	if got := m.Feed("q"); !reflect.DeepEqual(got, []Command{{Action: Bottom}}) {
		t.Fatalf("longer sequence = %v, want bottom", got)
	}
	m.Feed("g")
	if got := m.Feed("q"); !reflect.DeepEqual(got, []Command{{Action: Top}, {Action: Quit}}) {
		t.Fatalf("fallback and retry = %v, want top then quit", got)
	}
	m.Feed("g")
	m.Feed("z")
	if got := m.Feed("x"); !reflect.DeepEqual(got, []Command{{Action: Top}}) {
		t.Fatalf("longer incomplete prefix lost fallback: %v", got)
	}
}

func TestOverridesReplaceAliasesAndCanDisable(t *testing.T) {
	m, err := Compile(map[string][]string{"top": {"t", "t"}, "quit": {}})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []Key{"g", "g", "<Home>", "q", "z", "q", "<C-c>"} {
		if got := m.Feed(key); len(got) != 0 {
			t.Fatalf("replaced/disabled key %q fired %v", key, got)
		}
	}
	m.Feed("t")
	if got := m.Feed("t"); !reflect.DeepEqual(got, []Command{{Action: Top}}) {
		t.Fatalf("override = %v", got)
	}
}

func TestInvalidConfiguration(t *testing.T) {
	for name, overrides := range map[string]map[string][]string{
		"unknown action":              {"explode": {"x"}},
		"whole sequence in one token": {"top": {"gg"}},
		"duplicate explicit binding":  {"top": {"G"}, "bottom": {"G"}},
		"empty token":                 {"top": {""}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Compile(overrides); err == nil {
				t.Fatal("invalid keybind configuration was accepted")
			}
		})
	}
}

func TestRebindSpecialKeys(t *testing.T) {
	m, err := Compile(map[string][]string{"up": {"<C-c>"}, "top": {"<Esc>"}})
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[Key]Action{"<C-c>": Up, "<Esc>": Top, "q": Quit} {
		if got := m.Feed(key); !reflect.DeepEqual(got, []Command{{Action: want}}) {
			t.Fatalf("rebound %s = %v, want %s", key, got, want)
		}
	}
}

func TestSpecialKeysInSequences(t *testing.T) {
	m, err := Compile(map[string][]string{
		"top": {"<Esc>", "g"}, "bottom": {"<C-c>", "g"}, "cancel": {}, "quit": {},
	})
	if err != nil {
		t.Fatal(err)
	}
	for prefix, want := range map[Key]Action{"<Esc>": Top, "<C-c>": Bottom} {
		if got := m.Feed(prefix); len(got) != 0 || !m.Pending() {
			t.Fatalf("sequence prefix %s = %v", prefix, got)
		}
		if got := m.Feed("g"); !reflect.DeepEqual(got, []Command{{Action: want}}) {
			t.Fatalf("sequence %sg = %v, want %s", prefix, got, want)
		}
	}
}

func TestConfiguredSequenceOverridesDefaultAction(t *testing.T) {
	m, err := Compile(map[string][]string{"top": {"G"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Feed("G"); !reflect.DeepEqual(got, []Command{{Action: Top}}) {
		t.Fatalf("configured G = %v, want top", got)
	}
	if got := m.Feed("<End>"); !reflect.DeepEqual(got, []Command{{Action: Bottom}}) {
		t.Fatalf("unrelated default alias changed: %v", got)
	}
}

func TestParseKey(t *testing.T) {
	for token, want := range map[string]Key{"g": "g", "G": "G", "=": "=", "界": "界", "<C-F>": "<C-f>", "<Space>": " ", "<PageUp>": "<PageUp>"} {
		if got, err := ParseKey(token); got != want || err != nil {
			t.Errorf("ParseKey(%q) = (%q, %v), want %q", token, got, err, want)
		}
	}
}

func TestNumericPrefixes(t *testing.T) {
	for _, tt := range []struct {
		name      string
		keys      []Key
		overrides map[string][]string
		want      []Command
	}{
		{"line number", []Key{"5", "0", "G"}, nil, []Command{{Action: Bottom, Count: 50}}},
		{"plain bottom", []Key{"G"}, nil, []Command{{Action: Bottom}}},
		{"count resets after action", []Key{"5", "G", "G"}, nil, []Command{{Action: Bottom, Count: 5}, {Action: Bottom}}},
		{"cancel clears count", []Key{"5", "<Esc>", "G"}, nil, []Command{{Action: Cancel, Count: 5}, {Action: Bottom}}},
		{"unknown key clears count", []Key{"5", "x", "G"}, nil, []Command{{Action: Bottom}}},
		{"mismatch clears count", []Key{"5", "g", "G"}, nil, []Command{{Action: Bottom}}},
		{"rebound bottom", []Key{"5", "0", "z", "z"}, map[string][]string{"bottom": {"z", "z"}}, []Command{{Action: Bottom, Count: 50}}},
		{"digit binding wins", []Key{"5", "0"}, map[string][]string{"bottom": {"5", "0"}}, []Command{{Action: Bottom}}},
		{"digit inside sequence", []Key{"z", "5"}, map[string][]string{"bottom": {"z", "5"}}, []Command{{Action: Bottom}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m, err := Compile(tt.overrides)
			if err != nil {
				t.Fatal(err)
			}
			var got []Command
			for _, key := range tt.keys {
				got = append(got, m.Feed(key)...)
			}
			if !reflect.DeepEqual(got, tt.want) || m.Pending() {
				t.Fatalf("commands = %v, pending = %v; want %v", got, m.Pending(), tt.want)
			}
		})
	}
}

func TestNumericPrefixWaitsAndSaturates(t *testing.T) {
	m, _ := Compile(nil)
	for range 100 {
		if got := m.Feed("9"); len(got) != 0 || !m.Pending() || m.NeedsTimeout() {
			t.Fatalf("numeric prefix: commands = %v, pending = %v, timeout = %v", got, m.Pending(), m.NeedsTimeout())
		}
		if got := m.Flush(); len(got) != 0 || !m.Pending() {
			t.Fatal("numeric prefix expired")
		}
	}
	if got := m.Feed("G"); !reflect.DeepEqual(got, []Command{{Action: Bottom, Count: int(^uint(0) >> 1)}}) {
		t.Fatalf("overflowing count = %v", got)
	}
	m.Feed("5")
	m.Cancel()
	if m.Pending() {
		t.Fatal("Cancel left a numeric prefix pending")
	}
}

func TestCountedAmbiguousBinding(t *testing.T) {
	m, _ := Compile(map[string][]string{"bottom": {"g"}})
	m.Feed("5")
	m.Feed("0")
	m.Feed("g")
	if !m.NeedsTimeout() {
		t.Fatal("counted ambiguous binding needs a timeout")
	}
	if got := m.Flush(); !reflect.DeepEqual(got, []Command{{Action: Bottom, Count: 50}}) {
		t.Fatalf("counted timeout = %v", got)
	}
	m.Feed("5")
	m.Feed("g")
	if got := m.Feed("q"); !reflect.DeepEqual(got, []Command{{Action: Bottom, Count: 5}, {Action: Quit}}) {
		t.Fatalf("counted fallback and retry = %v", got)
	}
}

func TestSearchBindingsAndInsertion(t *testing.T) {
	m, err := CompileSearch(nil)
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[Key]Command{
		"5": {Action: Insert, Text: "5"}, "界": {Action: Insert, Text: "界"},
		"q": {Action: Insert, Text: "q"}, "/": {Action: Insert, Text: "/"},
		"<":     {Action: Insert, Text: "<"},
		"<M-b>": {Action: WordLeft}, "<M-f>": {Action: WordRight},
		"<C-w>": {Action: DeleteWordBackward}, "<M-Backspace>": {Action: DeleteWordBackward},
		"<M-d>": {Action: DeleteWordForward}, "<C-Left>": {Action: WordLeft},
		"<C-Right>": {Action: WordRight}, "<C-a>": {Action: Start}, "<C-e>": {Action: End},
		"<C-p>": {Action: HistoryPrevious}, "<C-n>": {Action: HistoryNext},
		"<Enter>": {Action: Accept}, "<Esc>": {Action: SearchCancel}, "<C-c>": {Action: SearchCancel},
	} {
		if got := m.Feed(key); !reflect.DeepEqual(got, []Command{want}) {
			t.Fatalf("%q = %v, want %v", key, got, want)
		}
	}
}

func TestSearchRebindingSequencesAndFallback(t *testing.T) {
	m, err := CompileSearch(map[string][]string{"search-word-left": {"x", "x"}, "search-cancel": {"<C-w>"}})
	if err != nil {
		t.Fatal(err)
	}
	m.Feed("x")
	if m.NeedsTimeout() || len(m.Flush()) != 0 || !m.Pending() {
		t.Fatal("search prefix expired")
	}
	if got := m.Feed("x"); !reflect.DeepEqual(got, []Command{{Action: WordLeft}}) {
		t.Fatalf("sequence = %v", got)
	}
	m.Feed("x")
	if got := m.Feed("y"); !reflect.DeepEqual(got, []Command{{Action: Insert, Text: "x"}, {Action: Insert, Text: "y"}}) {
		t.Fatalf("literal fallback = %v", got)
	}
	if got := m.Feed("<M-b>"); len(got) != 0 {
		t.Fatalf("replaced alias fired: %v", got)
	}
	if got := m.Feed("<C-w>"); !reflect.DeepEqual(got, []Command{{Action: SearchCancel}}) {
		t.Fatalf("rebound cancel = %v", got)
	}
	if _, err := CompileSearch(map[string][]string{"explode": {"x"}}); err == nil {
		t.Fatal("unknown action allowed in keybind map")
	}
	if _, err := CompileSearch(map[string][]string{"search-left": {"x"}, "search-right": {"x"}}); err == nil {
		t.Fatal("duplicate prompt binding accepted")
	}
}

func TestModifiedKeyNotation(t *testing.T) {
	for token, want := range map[string]Key{
		"<M-b>": "<M-b>", "<C-Left>": "<C-Left>", "<M-Backspace>": "<M-Backspace>",
		"<M-C-F>": "<C-M-f>", "<M-Space>": "<M- >", "<Delete>": "<Delete>",
	} {
		if got, err := ParseKey(token); got != want || err != nil {
			t.Fatalf("ParseKey(%q) = (%q, %v), want %q", token, got, err, want)
		}
	}
	for _, token := range []string{"<C-C-b>", "<M-unknown>", "<C->", "<M-gg>"} {
		if _, err := ParseKey(token); err == nil {
			t.Fatalf("accepted %q", token)
		}
	}
}

func TestUnifiedBindingsKeepModesIndependent(t *testing.T) {
	bindings := map[string][]string{
		"up": {"x"}, "search-left": {"x"},
		"cancel": {"<C-c>"}, "search-cancel": {"<C-w>"},
	}
	view, err := Compile(bindings)
	if err != nil {
		t.Fatal(err)
	}
	prompt, err := CompileSearch(bindings)
	if err != nil {
		t.Fatal(err)
	}
	if got := view.Feed("x"); !reflect.DeepEqual(got, []Command{{Action: Up}}) {
		t.Fatalf("view binding = %v", got)
	}
	if got := prompt.Feed("x"); !reflect.DeepEqual(got, []Command{{Action: Left}}) {
		t.Fatalf("prompt binding = %v", got)
	}
	if got := view.Feed("<C-c>"); !reflect.DeepEqual(got, []Command{{Action: Cancel}}) {
		t.Fatalf("view cancel = %v", got)
	}
	if got := prompt.Feed("<C-w>"); !reflect.DeepEqual(got, []Command{{Action: SearchCancel}}) {
		t.Fatalf("prompt cancel = %v", got)
	}
}
