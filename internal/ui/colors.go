package ui

import (
	"fmt"
	"strconv"

	"github.com/gdamore/tcell/v3"
	"github.com/gdamore/tcell/v3/color"
)

// Colors overrides only the supplied colors; empty fields inherit their base.
type Colors struct{ FG, BG string }

func (c Colors) Style(base tcell.Style) (tcell.Style, error) {
	for _, value := range []struct{ name, text string }{{"fg", c.FG}, {"bg", c.BG}} {
		if value.text == "" {
			continue
		}
		if len(value.text) != 7 || value.text[0] != '#' {
			return base, fmt.Errorf("%s must be a #RRGGBB hex color", value.name)
		}
		rgb, err := strconv.ParseUint(value.text[1:], 16, 24)
		if err != nil {
			return base, fmt.Errorf("%s must be a #RRGGBB hex color", value.name)
		}
		if value.name == "fg" {
			base = base.Foreground(color.NewHexColor(int32(rgb)))
		} else {
			base = base.Background(color.NewHexColor(int32(rgb)))
		}
	}
	return base, nil
}
