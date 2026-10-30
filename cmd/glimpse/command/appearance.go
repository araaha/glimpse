package command

import (
	"fmt"
	"slices"
	"strings"

	"glimpse/internal/app"
	"glimpse/internal/ui"
)

func readAppearance(cfg fileConfig, options *app.Options) error {
	statusline := cfg.Statusline
	options.Statusline.Left, options.Statusline.Right = statusline.Left, statusline.Right
	options.Statusline.SeparatorOpen, options.Statusline.SeparatorClose = statusline.SeparatorOpen, statusline.SeparatorClose
	options.Statusline.FG, options.Statusline.BG = statusline.FG, statusline.BG
	options.Statusline.FilenameStyle = statusline.FilenameStyle
	if !ui.ValidFilenameStyle(options.Statusline.FilenameStyle) {
		return fmt.Errorf("statusline.filename-style must be tail, short, or full")
	}
	var modes []string
	for _, mode := range ui.Modes {
		modes = append(modes, strings.ToLower(mode))
	}
	options.Statusline.Styles = make(map[string]ui.Colors)
	options.Statusline.ModeStyles = make(map[string]ui.Colors)
	for name, style := range statusline.Styles {
		options.Statusline.Styles[name] = ui.Colors{FG: style.FG, BG: style.BG}
		// Only the mode section has per-mode tables.
		for mode, colors := range style.Modes {
			if !strings.EqualFold(name, ui.StatusMode) || !slices.Contains(modes, mode) {
				return fmt.Errorf("unknown color option statusline.styles.%s.%s", name, mode)
			}
			options.Statusline.ModeStyles[mode] = colors
		}
	}
	options.SearchHighlight = cfg.Search.HighlightStyle
	options.LineNumberStyle = cfg.Display.LineNumberStyle
	options.UnprintableChar = cfg.Display.Unprintable.Char
	options.UnprintableStyle = ui.Colors{FG: cfg.Display.Unprintable.FG, BG: cfg.Display.Unprintable.BG}
	return nil
}
