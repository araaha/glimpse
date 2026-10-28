package app

import (
	"fmt"
	"strings"

	"glimpse/internal/keybind"
)

// helpLines lists the effective bindings, so configured keys and disabled
// actions are shown as they are, followed by the : commands. All sections
// share one key column.
func helpLines(view, prompt *keybind.Matcher) []string {
	type section struct {
		title string
		rows  [][2]string
	}
	viewActions, promptActions := keybind.Actions()
	var sections []section
	for _, mode := range []struct {
		title   string
		matcher *keybind.Matcher
		actions []keybind.Action
	}{{"View", view, viewActions}, {"Search and command prompt", prompt, promptActions}} {
		keys := make(map[keybind.Action][]string)
		for _, binding := range mode.matcher.Bindings() {
			keys[binding.Action] = append(keys[binding.Action], keybind.Format(binding.Keys))
		}
		current := section{title: mode.title}
		for _, action := range mode.actions {
			bound := strings.Join(keys[action], "  ")
			if bound == "" {
				bound = "(unbound)"
			}
			current.rows = append(current.rows, [2]string{bound, keybind.Descriptions[action]})
		}
		sections = append(sections, current)
	}
	sections = append(sections, section{"Commands", [][2]string{
		{":N", "go to line N"},
		{":top  :bottom", "go to the first or last line"},
		{":q  :quit", "quit"},
		{":set OPTION", "enable smart-case, ignore-case, case-sensitive,"},
		{":set no-OPTION", "  incremental, highlight, wrap, or line-numbers"},
		{":set jump-target=N", "place jumps on screen line N (.5 is the middle)"},
		{":set spool-quota=N", "raise the stream storage quota in bytes"},
	}})
	width := 0
	for _, section := range sections {
		for _, row := range section.rows {
			width = max(width, len(row[0]))
		}
	}
	lines := []string{"Glimpse keys (q or Esc closes this help)", ""}
	for _, section := range sections {
		lines = append(lines, section.title)
		for _, row := range section.rows {
			lines = append(lines, fmt.Sprintf("  %-*s  %s", width, row[0], row[1]))
		}
		lines = append(lines, "")
	}
	return append(lines, "A count before G goes to that line: 50G goes to line 50.")
}
