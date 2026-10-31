package app

import (
	"fmt"
	"strings"

	"github.com/araaha/glimpse/internal/keybind"
)

// helpLines lists the effective bindings, so configured keys and disabled
// actions are shown as they are, then configured run commands and the :
// commands. All sections share one key column.
func helpLines(view, prompt *keybind.Matcher, runs map[keybind.Action]RunCommand) []string {
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
	if len(runs) > 0 {
		current := section{title: "Run commands ($line and $file are set)"}
		for _, binding := range view.Bindings() {
			if run, ok := runs[binding.Action]; ok {
				command := run.Command
				if run.Background {
					command += "  (background)"
				}
				current.rows = append(current.rows, [2]string{keybind.Format(binding.Keys), command})
			}
		}
		sections = append(sections, current)
	}
	sections = append(sections, section{"Commands", [][2]string{
		{":N", "go to line N"},
		{":top  :bottom", "go to the first or last line"},
		{":q  :quit", "quit"},
		{":set OPTION", "enable ignorecase (ic), smartcase (scs),"},
		{":set noOPTION", "  incsearch (is), hlsearch (hls), magic,"},
		{"", "  wrap, number (nu), or hex-colors"},
		{"Tab  Shift-Tab", "complete a : command, as in vim"},
		{":set jump-target=N", "place jumps from 0 (bottom) to 1 (top); .5 is the middle"},
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
