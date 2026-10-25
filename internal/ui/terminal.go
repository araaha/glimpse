package ui

import (
	"strings"
	"unicode"

	"glimpse/internal/keybind"

	"github.com/gdamore/tcell/v3"
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
	// The separate search row is reserved but remains blank until search exists.
	t.screen.HideCursor()
	t.screen.Show()
	return nil
}

// KeyFromEvent keeps terminal decoding separate from sequence matching. Shift
// is already reflected in printable text ("G" must remain different from "g").
func KeyFromEvent(ev *tcell.EventKey) (keybind.Key, bool) {
	if !ev.Pressed() || ev.Modifiers()&(tcell.ModAlt|tcell.ModMeta) != 0 {
		return "", false
	}
	if ev.Key() >= tcell.KeyCtrlA && ev.Key() <= tcell.KeyCtrlZ {
		return keybind.Key("<C-" + string(rune('a'+ev.Key()-tcell.KeyCtrlA)) + ">"), true
	}
	if ev.Key() == tcell.KeyRune {
		text := ev.Str()
		if ev.Modifiers()&tcell.ModCtrl != 0 {
			text = "<C-" + text + ">"
		}
		key, err := keybind.ParseKey(text)
		return key, err == nil
	}
	name := map[tcell.Key]keybind.Key{
		tcell.KeyEsc:       "<Esc>",
		tcell.KeyUp:        "<Up>",
		tcell.KeyDown:      "<Down>",
		tcell.KeyLeft:      "<Left>",
		tcell.KeyRight:     "<Right>",
		tcell.KeyHome:      "<Home>",
		tcell.KeyEnd:       "<End>",
		tcell.KeyPgUp:      "<PageUp>",
		tcell.KeyPgDn:      "<PageDown>",
		tcell.KeyEnter:     "<Enter>",
		tcell.KeyTab:       "<Tab>",
		tcell.KeyBackspace: "<Backspace>",
	}[ev.Key()]
	return name, name != ""
}
