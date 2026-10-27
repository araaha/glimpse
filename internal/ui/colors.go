package ui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v3"
	"github.com/gdamore/tcell/v3/color"
)

// ansiColors names the 16 terminal palette entries. Palette colors follow the
// user's terminal theme, unlike fixed RGB values.
var ansiColors = func() map[string]color.Color {
	names := []string{"black", "red", "green", "yellow", "blue", "magenta", "cyan", "white"}
	colors := make(map[string]color.Color, 2*len(names))
	for i, name := range names {
		colors[name] = color.PaletteColor(i)
		colors["bright-"+name] = color.PaletteColor(i + 8)
	}
	return colors
}()

// Colors overrides only the supplied colors; empty fields inherit their base.
// Values are ANSI palette names (blue, bright-blue, ...) or #RRGGBB.
type Colors struct{ FG, BG string }

func (c Colors) Style(base tcell.Style) (tcell.Style, error) {
	if c.FG != "" {
		fg, err := parseColor(c.FG)
		if err != nil {
			return base, fmt.Errorf("fg %w", err)
		}
		base = base.Foreground(fg)
	}
	if c.BG != "" {
		bg, err := parseColor(c.BG)
		if err != nil {
			return base, fmt.Errorf("bg %w", err)
		}
		base = base.Background(bg)
	}
	return base, nil
}

func parseColor(text string) (color.Color, error) {
	if c, ok := ansiColors[strings.ReplaceAll(strings.ToLower(text), "_", "-")]; ok {
		return c, nil
	}
	if len(text) == 7 && text[0] == '#' {
		if rgb, err := strconv.ParseUint(text[1:], 16, 24); err == nil {
			return color.NewHexColor(int32(rgb)), nil
		}
	}
	return color.Default, fmt.Errorf("must be an ANSI color name (such as blue or bright-blue) or a #RRGGBB hex color")
}
