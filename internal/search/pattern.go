package search

import (
	"errors"
	"fmt"
	"regexp"
	"regexp/syntax"
	"strings"
	"unicode"
)

// MaxMatchBytes bounds a regular expression match on a very long line: lines
// are searched through a sliding window, so longer matches may be missed.
const MaxMatchBytes = 64 << 10

// Query is a search as typed, with the options that interpret it. Text is a
// regular expression (RE2 syntax) unless Literal is set. Anywhere in Text, \V
// makes the rest literal, and \c or \C forces case folding on or off.
type Query struct {
	Text                  string
	SmartCase, IgnoreCase bool
	Literal               bool // match the whole text literally, as after \V
}

// matcher is a compiled query: a rune literal searched with KMP, or a regular
// expression applied to one line at a time. Neither matches anything when
// both are empty.
type matcher struct {
	literal []rune
	fold    bool
	re      *regexp.Regexp
	// resume finds re's leftmost match after a one-rune prefix, which gives
	// ^ and \b their real context when a long line's window starts mid-line.
	resume *regexp.Regexp
}

// Validate reports why the query is not a valid pattern.
func (q Query) Validate() error {
	_, err := q.compile()
	return err
}

// IsRegex reports whether matches can be longer than the text or span a
// variable number of bytes, so callers must allow for a whole line of context.
func (q Query) IsRegex() bool {
	m, err := q.compile()
	return err == nil && m.re != nil
}

func (q Query) compile() (matcher, error) {
	source, fold := q.translate()
	if source == "" {
		return matcher{}, nil
	}
	re, err := regexp.Compile(source)
	if err != nil {
		var syntaxErr *syntax.Error
		if errors.As(err, &syntaxErr) {
			return matcher{}, fmt.Errorf("invalid pattern: %s", syntaxErr.Code)
		}
		return matcher{}, fmt.Errorf("invalid pattern: %w", err)
	}
	if prefix, complete := re.LiteralPrefix(); complete {
		literal := []rune(prefix)
		if fold {
			for i, r := range literal {
				literal[i] = foldRune(r)
			}
		}
		return matcher{literal: literal, fold: fold}, nil
	}
	flags := ""
	if fold {
		flags = "(?i)"
	}
	return matcher{
		re:     regexp.MustCompile(flags + source),
		resume: regexp.MustCompile(flags + `\A(?s:.)(?s:.*?)(` + source + ")"),
	}, nil
}

// translate turns the text into RE2 syntax, quoting literal sections, and
// decides case folding. Like vim's smartcase, an uppercase letter only counts
// when it is not part of an escape such as \S or \p{Lu}.
func (q Query) translate() (string, bool) {
	var source strings.Builder
	literal, upper, force := q.Literal, false, 0
	runes := []rune(q.Text)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if r == '\\' && i+1 < len(runes) {
			switch next := runes[i+1]; {
			case next == 'V':
				literal = true
				i++
				continue
			case next == 'c':
				force = 1 // \c wins over \C, as in vim.
				i++
				continue
			case next == 'C':
				if force == 0 {
					force = -1
				}
				i++
				continue
			case literal:
				source.WriteString(`\\`)
				if next == '\\' {
					i++
				}
				continue
			default:
				source.WriteRune(r)
				source.WriteRune(next)
				i++
				if (next == 'p' || next == 'P') && i+1 < len(runes) && runes[i+1] == '{' {
					for i+1 < len(runes) {
						i++
						source.WriteRune(runes[i])
						if runes[i] == '}' {
							break
						}
					}
				}
				continue
			}
		}
		upper = upper || unicode.IsUpper(r)
		if literal {
			source.WriteString(regexp.QuoteMeta(string(r)))
		} else {
			source.WriteRune(r)
		}
	}
	fold := (q.SmartCase || q.IgnoreCase) && !(q.SmartCase && upper)
	if force != 0 {
		fold = force > 0
	}
	return source.String(), fold
}
