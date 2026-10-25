package ui

import (
	"strings"
	"testing"

	"glimpse/internal/keybind"

	"github.com/gdamore/tcell/v3"
	"github.com/gdamore/tcell/v3/vt"
)

func TestKeyFromEvent(t *testing.T) {
	tests := []struct {
		event *tcell.EventKey
		want  keybind.Key
	}{
		{tcell.NewEventKey(tcell.KeyRune, "g", tcell.ModNone), "g"},
		{tcell.NewEventKey(tcell.KeyRune, "G", tcell.ModShift), "G"},
		{tcell.NewEventKey(tcell.KeyRune, "=", tcell.ModNone), "="},
		{tcell.NewEventKey(tcell.KeyCtrlF, "", tcell.ModCtrl), "<C-f>"},
		{tcell.NewEventKey(tcell.KeyCtrlC, "", tcell.ModCtrl), "<C-c>"},
		{tcell.NewEventKeyEx(tcell.KeyRune, "f", tcell.ModCtrl, true, 0, 1), "<C-f>"},
		{tcell.NewEventKeyEx(tcell.KeyRune, "c", tcell.ModCtrl, true, 0, 1), "<C-c>"},
		{tcell.NewEventKey(tcell.KeyEsc, "", tcell.ModNone), "<Esc>"},
		{tcell.NewEventKey(tcell.KeyUp, "", tcell.ModNone), "<Up>"},
		{tcell.NewEventKey(tcell.KeyRune, "界", tcell.ModNone), "界"},
		{tcell.NewEventKey(tcell.KeyRune, "g", tcell.ModAlt), ""},
		{tcell.NewEventKeyEx(tcell.KeyRune, "g", tcell.ModNone, false, 0, 1), ""},
	}
	for _, tt := range tests {
		got, ok := KeyFromEvent(tt.event)
		if got != tt.want || ok != (tt.want != "") {
			t.Errorf("KeyFromEvent(%s) = (%q, %v), want %q", tt.event.Name(), got, ok, tt.want)
		}
	}
}

func TestTerminalRendersAndClearsFooter(t *testing.T) {
	screen, err := tcell.NewTerminfoScreenFromTty(vt.NewMockTerm(vt.MockOptSize{X: 12, Y: 4}), tcell.OptNegotiation(false))
	if err != nil {
		t.Fatal(err)
	}
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()
	terminal := NewTerminal(screen)
	model, _ := NewModel(CmdHeightToggle)
	frame := Frame{Layout: model.Layout(12, 4), Lines: []string{"hello", "world"}, Status: "status"}
	terminal.Render(frame)
	if got := screenLine(screen, 0, 12); !strings.HasPrefix(got, "hello") {
		t.Fatalf("body = %q", got)
	}
	if got := screenLine(screen, 3, 12); !strings.HasPrefix(got, "status") {
		t.Fatalf("status = %q", got)
	}
	model.ToggleStatusline()
	terminal.Render(Frame{Layout: model.Layout(12, 4), Lines: []string{"new"}})
	if got := screenLine(screen, 3, 12); strings.TrimSpace(got) != "" {
		t.Fatalf("hidden statusbar was not cleared: %q", got)
	}
}

func screenLine(screen tcell.Screen, row, width int) string {
	var result strings.Builder
	for x := 0; x < width; x++ {
		text, _, _ := screen.Get(x, row)
		result.WriteString(text)
	}
	return result.String()
}
