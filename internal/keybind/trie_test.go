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
				} else if !reflect.DeepEqual(got, []Action{tt.want}) || matcher.Pending() {
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
	if got := m.Feed("j"); !reflect.DeepEqual(got, []Action{Down}) {
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
	if got := m.Feed("g"); !reflect.DeepEqual(got, []Action{Top}) || m.NeedsTimeout() {
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
	if got := m.Flush(); !reflect.DeepEqual(got, []Action{Top}) {
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
	if got := m.Feed("q"); !reflect.DeepEqual(got, []Action{Bottom}) {
		t.Fatalf("longer sequence = %v, want bottom", got)
	}
	m.Feed("g")
	if got := m.Feed("q"); !reflect.DeepEqual(got, []Action{Top, Quit}) {
		t.Fatalf("fallback and retry = %v, want top then quit", got)
	}
	m.Feed("g")
	m.Feed("z")
	if got := m.Feed("x"); !reflect.DeepEqual(got, []Action{Top}) {
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
	if got := m.Feed("t"); !reflect.DeepEqual(got, []Action{Top}) {
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
		if got := m.Feed(key); !reflect.DeepEqual(got, []Action{want}) {
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
		if got := m.Feed("g"); !reflect.DeepEqual(got, []Action{want}) {
			t.Fatalf("sequence %sg = %v, want %s", prefix, got, want)
		}
	}
}

func TestConfiguredSequenceOverridesDefaultAction(t *testing.T) {
	m, err := Compile(map[string][]string{"top": {"G"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Feed("G"); !reflect.DeepEqual(got, []Action{Top}) {
		t.Fatalf("configured G = %v, want top", got)
	}
	if got := m.Feed("<End>"); !reflect.DeepEqual(got, []Action{Bottom}) {
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
