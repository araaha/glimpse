package app

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"glimpse/internal/keybind"
	"glimpse/internal/ui"

	"github.com/gdamore/tcell/v3"
	"github.com/gdamore/tcell/v3/vt"
)

type recordingRenderer struct {
	terminal ui.Renderer
	frames   chan ui.Frame
}

func (r *recordingRenderer) Render(frame ui.Frame) error {
	if err := r.terminal.Render(frame); err != nil {
		return err
	}
	select {
	case r.frames <- frame:
	default:
	}
	return nil
}

func waitFrame(t *testing.T, frames <-chan ui.Frame, matches func(ui.Frame) bool) ui.Frame {
	t.Helper()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	for {
		select {
		case frame := <-frames:
			if matches(frame) {
				return frame
			}
		case <-timer.C:
			t.Fatal("timed out waiting for a matching terminal frame")
		}
	}
}

func TestRunStreamsScrollsAndTogglesStatus(t *testing.T) {
	application, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	screen, err := tcell.NewTerminfoScreenFromTty(vt.NewMockTerm(vt.MockOptSize{X: 8, Y: 4}), tcell.OptNegotiation(false))
	if err != nil {
		t.Fatal(err)
	}
	recorder := &recordingRenderer{ui.NewTerminal(screen), make(chan ui.Frame, 32)}
	application.Renderer = recorder
	input, writer := io.Pipe()
	defer writer.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		result <- application.run(ctx, input, "stdin", screen)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-stopped:
		case <-time.After(3 * time.Second):
			t.Error("viewer did not stop")
		}
	})
	// No newline and no EOF: the reader must publish partial input immediately.
	if _, err := io.WriteString(writer, "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGHIJKL"); err != nil {
		t.Fatal(err)
	}
	firstLine := func(want string) func(ui.Frame) bool {
		return func(frame ui.Frame) bool { return len(frame.Lines) > 0 && frame.Lines[0] == want }
	}
	waitFrame(t, recorder.frames, firstLine("abcdefgh"))
	key := func(text string) { screen.EventQ() <- tcell.NewEventKey(tcell.KeyRune, text, tcell.ModNone) }
	key("j")
	waitFrame(t, recorder.frames, firstLine("ijklmnop"))
	key("g")
	// A resize-triggered frame acknowledges that the first g was processed.
	// Pause beyond the ambiguity timeout to reproduce normal human typing.
	screen.EventQ() <- tcell.NewEventResize(8, 4)
	waitFrame(t, recorder.frames, firstLine("ijklmnop"))
	select {
	case <-time.After(4 * sequenceTimeout):
	case err := <-result:
		t.Fatalf("viewer exited while waiting for the second g: %v", err)
	}
	key("g")
	waitFrame(t, recorder.frames, firstLine("abcdefgh"))
	key("G")
	waitFrame(t, recorder.frames, firstLine("qrstuvwx"))
	key("=")
	waitFrame(t, recorder.frames, func(frame ui.Frame) bool {
		return frame.Layout.StatusRow == 3 && frame.Layout.Body.Height == 3 && firstLine("qrstuvwx")(frame)
	})
	key("j")
	waitFrame(t, recorder.frames, firstLine("yz012345"))
	key("=")
	waitFrame(t, recorder.frames, func(frame ui.Frame) bool {
		return frame.Layout.StatusRow == -1 && frame.Layout.Body.Height == 4 &&
			len(frame.Lines) == 4 && firstLine("qrstuvwx")(frame) && frame.Lines[3] == "EFGHIJKL"
	})
	key("j")
	waitFrame(t, recorder.frames, firstLine("qrstuvwx"))
	key("z")
	key("q")
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("zq did not quit the viewer")
	}
	if _, err := writer.Write([]byte("more")); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("quit did not close the blocked input reader: %v", err)
	}
}

func TestRunFollowsAndCtrlCQuitsPendingSequence(t *testing.T) {
	application, _ := New(Options{Follow: true})
	screen, err := tcell.NewTerminfoScreenFromTty(vt.NewMockTerm(vt.MockOptSize{X: 4, Y: 3}), tcell.OptNegotiation(false))
	if err != nil {
		t.Fatal(err)
	}
	recorder := &recordingRenderer{ui.NewTerminal(screen), make(chan ui.Frame, 32)}
	application.Renderer = recorder
	input, writer := io.Pipe()
	defer writer.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		result <- application.run(ctx, input, "stdin", screen)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-stopped:
		case <-time.After(3 * time.Second):
			t.Error("viewer did not stop")
		}
	})
	io.WriteString(writer, "abcdefghijkl")
	waitFrame(t, recorder.frames, func(frame ui.Frame) bool {
		return len(frame.Lines) == 3 && frame.Lines[0] == "abcd" && strings.Contains(frame.Status, "FOLLOW")
	})
	io.WriteString(writer, "mnop")
	waitFrame(t, recorder.frames, func(frame ui.Frame) bool {
		return len(frame.Lines) == 3 && frame.Lines[0] == "efgh"
	})
	screen.EventQ() <- tcell.NewEventKey(tcell.KeyRune, "=", tcell.ModNone)
	waitFrame(t, recorder.frames, func(frame ui.Frame) bool {
		return frame.Layout.StatusRow == 2 && len(frame.Lines) == 2 && frame.Lines[0] == "efgh"
	})
	io.WriteString(writer, "qrst")
	waitFrame(t, recorder.frames, func(frame ui.Frame) bool {
		return len(frame.Lines) == 2 && frame.Lines[0] == "mnop"
	})
	screen.EventQ() <- tcell.NewEventKey(tcell.KeyRune, "=", tcell.ModNone)
	waitFrame(t, recorder.frames, func(frame ui.Frame) bool {
		return frame.Layout.StatusRow == -1 && len(frame.Lines) == 3 && frame.Lines[0] == "ijkl" && frame.Lines[2] == "qrst"
	})
	screen.EventQ() <- tcell.NewEventKey(tcell.KeyRune, "g", tcell.ModNone)
	screen.EventQ() <- tcell.NewEventKey(tcell.KeyCtrlC, "", tcell.ModCtrl)
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Ctrl-C did not quit while a sequence was pending")
	}
}

func TestRunReboundCtrlCAndEscape(t *testing.T) {
	application, err := New(Options{Keybinds: map[string][]string{
		"up": {"<C-c>"}, "top": {"<Esc>"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	screen, err := tcell.NewTerminfoScreenFromTty(vt.NewMockTerm(vt.MockOptSize{X: 4, Y: 2}), tcell.OptNegotiation(false))
	if err != nil {
		t.Fatal(err)
	}
	recorder := &recordingRenderer{ui.NewTerminal(screen), make(chan ui.Frame, 32)}
	application.Renderer = recorder
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		result <- application.run(ctx, io.NopCloser(strings.NewReader("abcdefghijklmnop")), "test", screen)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-stopped:
		case <-time.After(3 * time.Second):
			t.Error("viewer did not stop")
		}
	})
	firstLine := func(want string) func(ui.Frame) bool {
		return func(frame ui.Frame) bool { return len(frame.Lines) > 0 && frame.Lines[0] == want }
	}
	waitFrame(t, recorder.frames, firstLine("abcd"))
	screen.EventQ() <- tcell.NewEventKey(tcell.KeyRune, "j", tcell.ModNone)
	waitFrame(t, recorder.frames, firstLine("efgh"))
	screen.EventQ() <- tcell.NewEventKey(tcell.KeyRune, "z", tcell.ModNone)
	screen.EventQ() <- tcell.NewEventKey(tcell.KeyCtrlC, "", tcell.ModCtrl)
	waitFrame(t, recorder.frames, firstLine("abcd"))
	screen.EventQ() <- tcell.NewEventKey(tcell.KeyRune, "G", tcell.ModNone)
	waitFrame(t, recorder.frames, firstLine("ijkl"))
	screen.EventQ() <- tcell.NewEventKey(tcell.KeyEsc, "", tcell.ModNone)
	waitFrame(t, recorder.frames, firstLine("abcd"))
	screen.EventQ() <- tcell.NewEventKey(tcell.KeyRune, "q", tcell.ModNone)
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("default q did not quit after rebinding Ctrl-C and Escape")
	}
}

func TestFooterExpansionFillsEOF(t *testing.T) {
	for _, follow := range []bool{false, true} {
		v := viewState{top: 6, height: 4, follow: follow}
		v.reflow(6, 10, 3, false) // Show the statusbar without scrolling.
		if v.top != 6 {
			t.Fatalf("showing footer moved top row (follow=%v): %+v", follow, v)
		}
		v.apply(keybind.Down, 10, 3)
		if v.top != 7 {
			t.Fatalf("down did not reach EOF: %+v", v)
		}
		v.reflow(7, 10, 3, false)
		v.reflow(7, 10, 4, false) // Hide it after scrolling down to EOF.
		if v.top != 6 || v.top+v.height != 10 {
			t.Fatalf("footer expansion left blank space at EOF: %+v", v)
		}
		v.reflow(v.top, 10, 4, false)
		if v.top != 6 {
			t.Fatalf("later repaint changed the filled viewport: %+v", v)
		}
		v.apply(keybind.Down, 10, 4)
		if v.top != 6 {
			t.Fatalf("down scrolled beyond the last full screen: %+v", v)
		}
	}
}

func TestFooterExpansionKeepsTopAwayFromEOF(t *testing.T) {
	v := viewState{top: 4, height: 3}
	v.reflow(4, 10, 4, false)
	if v.top != 4 {
		t.Fatalf("footer expansion moved top row away from EOF: %+v", v)
	}
}

func TestFooterExpansionWithNewInputDoesNotJumpToEnd(t *testing.T) {
	v := viewState{top: 7, height: 3}
	v.reflow(7, 20, 4, true)
	if v.top != 7 {
		t.Fatalf("footer expansion followed new input without follow mode: %+v", v)
	}
}

func TestFollowReanchorsOnNewDataOrExplicitAction(t *testing.T) {
	v := viewState{top: 7, follow: true}
	v.reflow(7, 12, 4, true)
	if v.top != 8 {
		t.Fatalf("new input did not advance follow view: %+v", v)
	}
	v = viewState{top: 7}
	v.apply(keybind.Follow, 10, 4)
	v.reflow(7, 10, 4, false)
	if !v.follow || v.top != 6 {
		t.Fatalf("explicit F did not jump to the end: %+v", v)
	}
}

func TestNavigation(t *testing.T) {
	v := viewState{follow: true}
	v.apply(keybind.Down, 20, 5)
	if v.top != 1 || v.follow {
		t.Fatalf("j: %+v", v)
	}
	v.apply(keybind.PageDown, 20, 5)
	if v.top != 6 {
		t.Fatalf("Ctrl-F: %+v", v)
	}
	v.apply(keybind.HalfPageUp, 20, 5)
	if v.top != 4 {
		t.Fatalf("Ctrl-U: %+v", v)
	}
	v.apply(keybind.Bottom, 20, 5)
	if v.top != 15 || !v.bottom {
		t.Fatalf("G: %+v", v)
	}
	v.apply(keybind.Up, 20, 5)
	if v.top != 14 || v.bottom {
		t.Fatalf("G followed by k: %+v", v)
	}
	v.apply(keybind.Top, 20, 5)
	v.apply(keybind.Up, 20, 5)
	if v.top != 0 {
		t.Fatalf("scrolling before start: %+v", v)
	}
	v.apply(keybind.Follow, 20, 5)
	if !v.follow {
		t.Fatal("F did not enable following")
	}
}
