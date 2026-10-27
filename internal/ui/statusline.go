package ui

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/clipperhouse/displaywidth"
	"github.com/clipperhouse/uax29/v2/graphemes"
	"github.com/gdamore/tcell/v3"
)

const (
	StatusMode          = "mode"
	StatusFilename      = "filename"
	StatusSearchResults = "search-results"
	StatusLineNumbers   = "line-numbers"
)

type StatuslineConfig struct {
	Left, Right []string // nil inherits defaults; an empty array disables a side.
	Separator   *string  // nil inherits the default; an empty string is valid.
	FG, BG      string
	Styles      map[string]Colors
}

type Statusline struct {
	left, right []string
	separator   string
	base        tcell.Style
	styles      map[string]tcell.Style
}

// StatusState is immutable frame data, never a reference to a mutable index.
type StatusState struct {
	Mode, Filename                                  string
	HasSearch, SearchTotalKnown, SearchCurrentKnown bool
	SearchCurrent, SearchTotal                      int64
	TopLine, TotalLines                             int
	TotalLinesKnown                                 bool
}

type StatusSpan struct {
	Column, Width int
	Text, Section string
	Style         tcell.Style
}

type StatusBar struct {
	Width int
	Style tcell.Style
	Spans []StatusSpan
}

func NewStatusline(config StatuslineConfig) (*Statusline, error) {
	base, _ := (Colors{FG: "white", BG: "black"}).Style(tcell.StyleDefault)
	base, err := (Colors{FG: config.FG, BG: config.BG}).Style(base)
	if err != nil {
		return nil, fmt.Errorf("statusline: %w", err)
	}
	s := &Statusline{base: base, separator: " │ ", styles: make(map[string]tcell.Style)}
	defaults := map[string]Colors{
		StatusMode:          {FG: "black", BG: "blue"},
		StatusFilename:      {FG: "blue"},
		StatusSearchResults: {FG: "black", BG: "white"},
		StatusLineNumbers:   {FG: "black", BG: "blue"},
	}
	for name, colors := range defaults {
		s.styles[name], _ = colors.Style(base)
	}
	seenStyles := make(map[string]bool)
	for name, colors := range config.Styles {
		section := normalizeSection(name)
		style, ok := s.styles[section]
		if !ok {
			return nil, fmt.Errorf("unknown statusline section %q", name)
		}
		if seenStyles[section] {
			return nil, fmt.Errorf("duplicate statusline style %q", name)
		}
		seenStyles[section] = true
		style, err = colors.Style(style)
		if err != nil {
			return nil, fmt.Errorf("statusline.styles.%s: %w", name, err)
		}
		s.styles[section] = style
	}
	left, right := config.Left, config.Right
	if left == nil {
		left = []string{StatusMode, StatusFilename}
	}
	if right == nil {
		right = []string{StatusSearchResults, StatusLineNumbers}
	}
	seen := make(map[string]bool)
	for _, side := range []struct {
		input  []string
		output *[]string
	}{{left, &s.left}, {right, &s.right}} {
		for _, name := range side.input {
			section := normalizeSection(name)
			if _, ok := defaults[section]; !ok {
				return nil, fmt.Errorf("unknown statusline section %q", name)
			}
			if seen[section] {
				return nil, fmt.Errorf("duplicate statusline section %q", name)
			}
			seen[section] = true
			*side.output = append(*side.output, section)
		}
	}
	if config.Separator != nil {
		if len(*config.Separator) > 256 {
			return nil, fmt.Errorf("statusline.separator must be at most 256 bytes")
		}
		for _, r := range *config.Separator {
			if unicode.IsControl(r) {
				return nil, fmt.Errorf("statusline.separator cannot contain control characters")
			}
		}
		s.separator = *config.Separator
	}
	return s, nil
}

func normalizeSection(name string) string { return strings.ToLower(strings.ReplaceAll(name, "_", "-")) }

func (s *Statusline) Has(section string) bool {
	for _, side := range [][]string{s.left, s.right} {
		for _, name := range side {
			if name == section {
				return true
			}
		}
	}
	return false
}

func safeStatusText(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return '?'
		}
		return r
	}, text)
}

func statusWidth(text string, wide bool) int {
	return (displaywidth.Options{EastAsianWidth: wide}).String(text)
}

func clipStatus(text string, width int, wide bool) string {
	if width <= 0 {
		return ""
	}
	if statusWidth(text, wide) <= width {
		return text
	}
	ellipsis := "…"
	budget := width - statusWidth(ellipsis, wide)
	if budget < 0 {
		return ""
	}
	var out strings.Builder
	used := 0
	iterator := graphemes.FromString(text)
	for iterator.Next() {
		part := iterator.Value()
		w := statusWidth(part, wide)
		if used+w > budget {
			break
		}
		out.WriteString(part)
		used += w
	}
	return out.String() + ellipsis
}

func (s *Statusline) Format(state StatusState, width int, wide bool) *StatusBar {
	width = max(0, width)
	bar := &StatusBar{Width: width, Style: s.base}
	values := map[string]string{StatusMode: safeStatusText(state.Mode), StatusFilename: safeStatusText(state.Filename)}
	if values[StatusMode] == "" {
		values[StatusMode] = "VIEW"
	}
	if state.HasSearch {
		current, total := "…", "…"
		if state.SearchCurrentKnown {
			current = fmt.Sprint(state.SearchCurrent)
		}
		if state.SearchTotalKnown {
			total = fmt.Sprint(state.SearchTotal)
		}
		values[StatusSearchResults] = current + "/" + total
	}
	total := "…"
	if state.TotalLinesKnown {
		total = fmt.Sprint(state.TotalLines)
	}
	values[StatusLineNumbers] = fmt.Sprintf("%d/%s", state.TopLine, total)
	pad := " "
	build := func(names []string) []StatusSpan {
		var spans []StatusSpan
		column := 0
		for _, name := range names {
			value := values[name]
			if value == "" {
				continue
			}
			if len(spans) > 0 && s.separator != "" {
				w := statusWidth(s.separator, wide)
				spans = append(spans, StatusSpan{Column: column, Width: w, Text: s.separator, Style: s.base})
				column += w
			}
			// Pad inside the span so the section background frames its text.
			value = pad + value + pad
			w := statusWidth(value, wide)
			spans = append(spans, StatusSpan{Column: column, Width: w, Text: value, Section: name, Style: s.styles[name]})
			column += w
		}
		return spans
	}
	spanWidth := func(spans []StatusSpan) int {
		if len(spans) == 0 {
			return 0
		}
		last := spans[len(spans)-1]
		return last.Column + last.Width
	}
	var left, right []StatusSpan
	groupWidth := func() int {
		n := spanWidth(left) + spanWidth(right)
		if len(left) > 0 && len(right) > 0 {
			n++
		}
		return n
	}
	filename := values[StatusFilename]
	fit := func() {
		values[StatusFilename] = filename
		left, right = build(s.left), build(s.right)
		if overflow := groupWidth() - width; overflow > 0 {
			// Filename is always the first field sacrificed to a narrow terminal,
			// regardless of which side the user placed it on.
			values[StatusFilename] = clipStatus(filename, statusWidth(filename, wide)-overflow, wide)
			left, right = build(s.left), build(s.right)
		}
	}
	fit()
	if groupWidth() > width {
		// Padding is cosmetic; drop it before hiding any section.
		pad = ""
		fit()
	}
	if groupWidth() > width && s.Has(StatusMode) {
		// Keep the mode and line position usable before sacrificing them to an
		// optional search counter on very narrow terminals.
		values[StatusSearchResults] = ""
		fit()
	}
	gap := 0
	if len(left) > 0 && len(right) > 0 {
		gap = 1
	}
	leftLimit := width
	if len(right) > 0 {
		leftLimit = max(0, width-spanWidth(right)-gap)
	}
	appendClipped := func(spans []StatusSpan, shift, limit int) {
		for _, span := range spans {
			column := shift + span.Column
			var text strings.Builder
			start, used := -1, 0
			iterator := graphemes.FromString(span.Text)
			for iterator.Next() {
				part := iterator.Value()
				w := statusWidth(part, wide)
				if column >= 0 && column+w <= limit {
					if start < 0 {
						start = column
					}
					text.WriteString(part)
					used += w
				}
				column += w
				if column >= limit {
					break
				}
			}
			if start >= 0 {
				span.Column, span.Width, span.Text = start, used, text.String()
				bar.Spans = append(bar.Spans, span)
			}
		}
	}
	appendClipped(left, 0, leftLimit)
	appendClipped(right, width-spanWidth(right), width)
	return bar
}

// Has reports whether a section survived formatting and is visible.
func (b *StatusBar) Has(section string) bool {
	for _, span := range b.Spans {
		if span.Section == section {
			return true
		}
	}
	return false
}

func (b *StatusBar) Text() string {
	var out strings.Builder
	column := 0
	for _, span := range b.Spans {
		out.WriteString(strings.Repeat(" ", max(0, span.Column-column)))
		out.WriteString(span.Text)
		column = span.Column + span.Width
	}
	out.WriteString(strings.Repeat(" ", max(0, b.Width-column)))
	return out.String()
}
