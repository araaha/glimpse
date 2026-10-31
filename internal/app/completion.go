package app

import (
	"slices"
	"strings"
)

// commandNames and setOptions are the : command words offered by completion.
// Options use vim's names. Those taking a value end in "="; the others also
// have a no form, offered once the word being completed starts like it.
var (
	commandNames = []string{"bottom", "filter", "nofilter", "q", "quit", "set", "top"}
	setOptions   = []string{"hex-colors", "hlsearch", "ignorecase", "incsearch", "jump-target=", "magic", "number", "smartcase", "spool-quota=", "wrap"}
)

// completion cycles vim style: the first step replaces the word before the
// cursor with the first candidate, and stepping past the last candidate
// restores the typed word.
type completion struct {
	start      int // rune index of the completed word
	word       string
	candidates []string
	index      int // len(candidates) while the typed word is shown
}

// commandCompletions returns where the word before cursor starts and the
// command words it can complete to: a command name first, then a set option.
func commandCompletions(text []rune, cursor int) (int, []string) {
	start := cursor
	for start > 0 && text[start-1] != ' ' {
		start--
	}
	word, before := string(text[start:cursor]), strings.Fields(string(text[:start]))
	var words []string
	switch {
	case len(before) == 0:
		words = commandNames
	case len(before) == 1 && before[0] == "set":
		words = setOptions
		if word != "" && (strings.HasPrefix(word, "no") || strings.HasPrefix("no", word)) {
			for _, option := range setOptions {
				if !strings.HasSuffix(option, "=") {
					words = append(slices.Clip(words), "no"+option)
				}
			}
			slices.Sort(words)
		}
	}
	var candidates []string
	for _, candidate := range words {
		if strings.HasPrefix(candidate, word) {
			candidates = append(candidates, candidate)
		}
	}
	return start, candidates
}

// ghost is the rest of the first completion of the word before the cursor,
// shown before Tab is pressed. It needs the cursor at the end, so it never
// covers typed text, and a nonempty word, so an empty prompt stays clean.
func (p *prompt) ghost() string {
	if !p.command || p.completion != nil || p.cursor != len(p.text) {
		return ""
	}
	start, candidates := commandCompletions(p.text, p.cursor)
	if word := string(p.text[start:]); word != "" && len(candidates) > 0 {
		return strings.TrimPrefix(candidates[0], word)
	}
	return ""
}

// complete steps through command completions, forward or backward.
func (p *prompt) complete(forward bool) {
	if p.completion == nil {
		start, candidates := commandCompletions(p.text, p.cursor)
		if len(candidates) == 0 {
			return
		}
		p.completion = &completion{start: start, word: string(p.text[start:p.cursor]), candidates: candidates, index: len(candidates)}
	}
	c := p.completion
	if forward {
		c.index = (c.index + 1) % (len(c.candidates) + 1)
	} else {
		c.index = (c.index + len(c.candidates)) % (len(c.candidates) + 1)
	}
	replacement := []rune(c.word)
	if c.index < len(c.candidates) {
		replacement = []rune(c.candidates[c.index])
	}
	p.text = append(append(append([]rune(nil), p.text[:c.start]...), replacement...), p.text[p.cursor:]...)
	p.cursor = c.start + len(replacement)
}
