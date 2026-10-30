package ui

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"glimpse/internal/content"
	"glimpse/internal/keybind"

	"github.com/clipperhouse/displaywidth"
	"github.com/clipperhouse/uax29/v2/graphemes"
	"github.com/gdamore/tcell/v3"
	"github.com/gdamore/tcell/v3/color"
)

type Terminal struct{ screen tcell.Screen }

func NewTerminal(screen tcell.Screen) *Terminal { return &Terminal{screen: screen} }

func (t *Terminal) Render(frame Frame) error {
	t.screen.Fill(' ', tcell.StyleDefault)
	gutter := frame.GutterWidth
	for y, number := range frame.Gutter {
		if y >= frame.Layout.Body.Height {
			break
		}
		t.screen.FillArea(0, y, gutter, 1, ' ', frame.GutterStyle)
		if len(number) > gutter-1 {
			number = number[len(number)-(gutter-1):]
		}
		t.screen.PutStrStyled(gutter-1-len(number), y, number, frame.GutterStyle)
	}
	for y, line := range frame.Lines {
		if y >= frame.Layout.Body.Height {
			break
		}
		if y < len(frame.Styled) {
			continue
		}
		t.screen.PutStr(gutter, y, line)
	}
	for row, glyphs := range frame.Styled {
		if row >= frame.Layout.Body.Height {
			break
		}
		for _, glyph := range glyphs {
			if gutter+glyph.Column < frame.Layout.Body.Width {
				style := terminalStyle(glyph.Style, glyph.UnderlineColor)
				if glyph.Unprintable {
					style = withColors(style, frame.UnprintableStyle)
				}
				t.screen.Put(gutter+glyph.Column, row, glyph.Text, style)
			}
		}
	}
	for _, code := range frame.HexColors {
		if code.Row < 0 || code.Row >= frame.Layout.Body.Height {
			continue
		}
		for x := gutter + max(0, code.Column); x < min(frame.Layout.Body.Width, gutter+code.Column+code.Width); {
			text, style, width := t.screen.Get(x, code.Row)
			if width > 0 {
				t.screen.Put(x, code.Row, text, style.Reverse(false).Foreground(contrastColor(code.RGB)).Background(color.NewHexColor(code.RGB)))
			}
			x += max(1, width)
		}
	}
	for _, mark := range frame.Highlights {
		if mark.Row < 0 || mark.Row >= frame.Layout.Body.Height {
			continue
		}
		for x := gutter + max(0, mark.Column); x < min(frame.Layout.Body.Width, gutter+mark.Column+mark.Width); {
			text, style, width := t.screen.Get(x, mark.Row)
			if width > 0 {
				highlight := frame.HighlightStyle
				fg, bg := highlight.GetForeground(), highlight.GetBackground()
				if fg.Valid() || bg.Valid() {
					style = style.Reverse(false)
					if fg.Valid() {
						style = style.Foreground(fg)
					}
					if bg.Valid() {
						style = style.Background(bg)
					}
				} else {
					style = style.Reverse(true)
				}
				t.screen.Put(x, mark.Row, text, style)
			}
			x += max(1, width)
		}
	}
	if frame.Layout.StatusRow >= 0 {
		if frame.StatusBar != nil {
			t.screen.FillArea(0, frame.Layout.StatusRow, frame.Layout.Body.Width, 1, ' ', frame.StatusBar.Style)
			for _, span := range frame.StatusBar.Spans {
				t.screen.FillArea(span.Column, frame.Layout.StatusRow, span.Width, 1, ' ', span.Style)
				column := span.Column
				iterator := graphemes.FromString(span.Text)
				for iterator.Next() {
					part := iterator.Value()
					t.screen.Put(column, frame.Layout.StatusRow, part, span.Style)
					column += statusWidth(part, frame.WideAmbiguous)
				}
			}
		} else {
			style := tcell.StyleDefault.Reverse(true)
			t.screen.FillArea(0, frame.Layout.StatusRow, frame.Layout.Body.Width, 1, ' ', style)
			status := strings.Map(func(r rune) rune {
				if unicode.IsControl(r) {
					return '?'
				}
				return r
			}, frame.Status)
			t.screen.PutStrStyled(0, frame.Layout.StatusRow, clipStatus(status, frame.Layout.Body.Width, frame.WideAmbiguous), style)
		}
	}
	t.screen.HideCursor()
	if frame.Layout.SearchRow >= 0 {
		text, cursor := searchLine(frame.Search, frame.SearchCursor, frame.Layout.Body.Width, frame.WideAmbiguous)
		t.screen.PutStr(0, frame.Layout.SearchRow, text)
		if frame.Ghost != "" {
			used := (displaywidth.Options{EastAsianWidth: frame.WideAmbiguous}).String(text)
			ghost := frame.Ghost[:min(len(frame.Ghost), max(0, frame.Layout.Body.Width-used))]
			t.screen.PutStrStyled(used, frame.Layout.SearchRow, ghost, tcell.StyleDefault.Dim(true))
		}
		if frame.SearchActive && frame.Layout.Body.Width > 0 {
			t.screen.ShowCursor(cursor, frame.Layout.SearchRow)
		}
	}
	t.screen.Show()
	return nil
}

// contrastColor picks black or white text for a background, by its relative
// luminance with the sRGB coefficients approximated as integers.
func contrastColor(rgb int32) color.Color {
	r, g, b := rgb>>16&255, rgb>>8&255, rgb&255
	if 299*r+587*g+114*b >= 128000 {
		return color.NewHexColor(0x000000)
	}
	return color.NewHexColor(0xffffff)
}

// withColors applies the valid foreground and background of colors to style.
func withColors(style, colors tcell.Style) tcell.Style {
	if fg := colors.GetForeground(); fg.Valid() {
		style = style.Foreground(fg)
	}
	if bg := colors.GetBackground(); bg.Valid() {
		style = style.Background(bg)
	}
	return style
}

func terminalStyle(style content.Style, underline ...content.Color) tcell.Style {
	result := tcell.StyleDefault.Bold(style&content.Bold != 0).Dim(style&content.Dim != 0).Italic(style&content.Italic != 0).Underline(style&content.Underline != 0).Reverse(style&content.Reverse != 0).StrikeThrough(style&content.Strike != 0)
	result = result.Foreground(terminalColor(style.ForegroundColor())).Background(terminalColor(style.BackgroundColor()))
	if len(underline) > 0 {
		if underline[0] != 0 {
			result = result.Underline(terminalColor(underline[0]))
		}
	}
	return result
}

func terminalColor(value content.Color) color.Color {
	if index, ok := value.Index(); ok {
		return color.PaletteColor(index)
	}
	if rgb, ok := value.RGB(); ok {
		return color.NewRGBColor(int32(rgb>>16), int32(rgb>>8&255), int32(rgb&255))
	}
	return color.Default
}

// searchLine keeps the editing cursor visible without splitting UTF-8. Controls
// from history/errors are rendered visibly, never sent as terminal commands.
func searchLine(text string, cursor, width int, wide bool) (string, int) {
	if width <= 0 {
		return "", 0
	}
	runes := []rune(text)
	cursor = min(max(0, cursor), len(runes))
	var parts []string
	var widths []int
	column := 0
	position, clusterCursor := 0, 0
	iterator := graphemes.FromString(text)
	for iterator.Next() {
		part := iterator.Value()
		var safe strings.Builder
		for _, r := range part {
			if unicode.IsControl(r) {
				safe.WriteString(strconv.QuoteRune(r))
			} else {
				safe.WriteRune(r)
			}
		}
		parts = append(parts, safe.String())
		w := (displaywidth.Options{EastAsianWidth: wide}).String(safe.String())
		widths = append(widths, w)
		if position < cursor {
			column += w
			clusterCursor++
		}
		position += utf8.RuneCountInString(part)
	}
	start := 0
	for start < clusterCursor && column >= width {
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

// KeysFromEvent accepts composed graphemes delivered in one terminal event,
// while retaining the existing one-rune configurable binding token convention.
func KeysFromEvent(ev *tcell.EventKey) []keybind.Key {
	if !ev.Pressed() {
		return nil
	}
	if ev.Key() == tcell.KeyRune && ev.Modifiers()&(tcell.ModCtrl|tcell.ModAlt|tcell.ModMeta) == 0 && utf8.ValidString(ev.Str()) {
		var keys []keybind.Key
		for _, r := range ev.Str() {
			if key, err := keybind.ParseKey(string(r)); err == nil {
				keys = append(keys, key)
			}
		}
		return keys
	}
	if key, ok := KeyFromEvent(ev); ok {
		return []keybind.Key{key}
	}
	return nil
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
		tcell.KeyBacktab:    "<S-Tab>",
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
