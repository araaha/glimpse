package ui

import (
	"strconv"
	"strings"
	"unicode"

	"glimpse/internal/content"
	"glimpse/internal/keybind"

	"github.com/clipperhouse/displaywidth"
	"github.com/gdamore/tcell/v3"
	"github.com/gdamore/tcell/v3/color"
)

type Terminal struct{ screen tcell.Screen }

func NewTerminal(screen tcell.Screen) *Terminal { return &Terminal{screen: screen} }

func (t *Terminal) Render(frame Frame) error {
	t.screen.Fill(' ', tcell.StyleDefault)
	for y, line := range frame.Lines {
		if y >= frame.Layout.Body.Height {
			break
		}
		t.screen.PutStr(0, y, line)
	}
	for row, glyphs := range frame.Styled {
		if row >= frame.Layout.Body.Height {
			break
		}
		for _, glyph := range glyphs {
			if glyph.Column < frame.Layout.Body.Width {
				t.screen.Put(glyph.Column, row, glyph.Text, terminalStyle(glyph.Style))
			}
		}
	}
	for _, mark := range frame.Highlights {
		if mark.Row < 0 || mark.Row >= frame.Layout.Body.Height {
			continue
		}
		for x := max(0, mark.Column); x < min(frame.Layout.Body.Width, mark.Column+mark.Width); {
			text, style, width := t.screen.Get(x, mark.Row)
			if width > 0 {
				t.screen.Put(x, mark.Row, text, style.Reverse(true))
			}
			x += max(1, width)
		}
	}
	if frame.Layout.StatusRow >= 0 {
		style := tcell.StyleDefault.Reverse(true)
		t.screen.FillArea(0, frame.Layout.StatusRow, frame.Layout.Body.Width, 1, ' ', style)
		status := strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return '?'
			}
			return r
		}, frame.Status)
		t.screen.PutStrStyled(0, frame.Layout.StatusRow, status, style)
	}
	t.screen.HideCursor()
	if frame.Layout.SearchRow >= 0 {
		text, cursor := searchLine(frame.Search, frame.SearchCursor, frame.Layout.Body.Width)
		t.screen.PutStr(0, frame.Layout.SearchRow, text)
		if frame.SearchActive && frame.Layout.Body.Width > 0 {
			t.screen.ShowCursor(cursor, frame.Layout.SearchRow)
		}
	}
	t.screen.Show()
	return nil
}

func terminalStyle(style content.Style) tcell.Style {
	result := tcell.StyleDefault.Bold(style&content.Bold != 0).Dim(style&content.Dim != 0).Italic(style&content.Italic != 0).Underline(style&content.Underline != 0).Reverse(style&content.Reverse != 0).StrikeThrough(style&content.Strike != 0)
	if rgb, ok := style.Foreground(); ok {
		result = result.Foreground(color.NewRGBColor(int32(rgb>>16), int32(rgb>>8&255), int32(rgb&255)))
	}
	if rgb, ok := style.Background(); ok {
		result = result.Background(color.NewRGBColor(int32(rgb>>16), int32(rgb>>8&255), int32(rgb&255)))
	}
	return result
}

// searchLine keeps the editing cursor visible without splitting UTF-8. Controls
// from history/errors are rendered visibly, never sent as terminal commands.
func searchLine(text string, cursor, width int) (string, int) {
	if width <= 0 {
		return "", 0
	}
	runes := []rune(text)
	cursor = min(max(0, cursor), len(runes))
	parts := make([]string, len(runes))
	widths := make([]int, len(runes))
	column := 0
	for i, r := range runes {
		parts[i] = string(r)
		if unicode.IsControl(r) {
			parts[i] = strconv.QuoteRune(r)
		}
		widths[i] = displaywidth.String(parts[i])
		if i < cursor {
			column += widths[i]
		}
	}
	start := 0
	for start < cursor && column >= width {
		column -= widths[start]
		start++
	}
	var out strings.Builder
	used := 0
	for i := start; i < len(parts) && used+widths[i] <= width; i++ {
		out.WriteString(parts[i])
		used += widths[i]
	}
	return out.String(), min(column, width-1)
}

// KeyFromEvent keeps terminal decoding separate from sequence matching. Shift
// is already reflected in printable text ("G" must remain different from "g").
func KeyFromEvent(ev *tcell.EventKey) (keybind.Key, bool) {
	if !ev.Pressed() {
		return "", false
	}
	modifier := ""
	if ev.Modifiers()&(tcell.ModAlt|tcell.ModMeta) != 0 {
		modifier = "M-"
	}
	modified := func(key keybind.Key) (keybind.Key, bool) {
		if modifier == "" {
			return key, key != ""
		}
		base := string(key)
		if len(base) > 1 && strings.HasPrefix(base, "<") && strings.HasSuffix(base, ">") {
			base = base[1 : len(base)-1]
		}
		if strings.HasPrefix(base, "C-") {
			base = "C-M-" + strings.TrimPrefix(base, "C-")
		} else {
			base = modifier + base
		}
		parsed, err := keybind.ParseKey("<" + base + ">")
		return parsed, err == nil
	}
	if ev.Key() >= tcell.KeyCtrlA && ev.Key() <= tcell.KeyCtrlZ {
		return modified(keybind.Key("<C-" + string(rune('a'+ev.Key()-tcell.KeyCtrlA)) + ">"))
	}
	if ev.Key() == tcell.KeyRune {
		text := ev.Str()
		if ev.Modifiers()&tcell.ModCtrl != 0 {
			text = "<C-" + text + ">"
		}
		key, err := keybind.ParseKey(text)
		if err != nil {
			return "", false
		}
		return modified(key)
	}
	name := map[tcell.Key]keybind.Key{
		tcell.KeyEsc:        "<Esc>",
		tcell.KeyUp:         "<Up>",
		tcell.KeyDown:       "<Down>",
		tcell.KeyLeft:       "<Left>",
		tcell.KeyRight:      "<Right>",
		tcell.KeyHome:       "<Home>",
		tcell.KeyEnd:        "<End>",
		tcell.KeyPgUp:       "<PageUp>",
		tcell.KeyPgDn:       "<PageDown>",
		tcell.KeyEnter:      "<Enter>",
		tcell.KeyTab:        "<Tab>",
		tcell.KeyBackspace:  "<Backspace>",
		tcell.KeyBackspace2: "<Backspace>",
		tcell.KeyDelete:     "<Delete>",
	}[ev.Key()]
	if name == "" {
		return "", false
	}
	if ev.Modifiers()&tcell.ModCtrl != 0 {
		name = keybind.Key("<C-" + strings.Trim(string(name), "<>") + ">")
	}
	return modified(name)
}
