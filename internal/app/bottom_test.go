package app

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"glimpse/internal/ui"

	"github.com/gdamore/tcell/v3"
	"github.com/gdamore/tcell/v3/vt"
)

type gatedBottomRenderer struct {
	*recordingRenderer
	gate  <-chan struct{}
	first bool
}

func (r *gatedBottomRenderer) Render(frame ui.Frame) error {
	err := r.recordingRenderer.Render(frame)
	if r.first {
		r.first = false
		<-r.gate
	}
	return err
}

// Preload a styled input that takes many indexing slices, without relying on
// ingestion speed or an external file. Keep the pipe open to exercise live EOF.
func startBottomViewer(t *testing.T, options Options, tail string) (testViewer, *io.PipeWriter) {
	t.Helper()
	application, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	var input strings.Builder
	for i := range 200000 {
		fmt.Fprintf(&input, "\x1b[32mrow%06d\x1b[m\n", i)
	}
	input.WriteString(tail)
	if _, err := application.Document.Write([]byte(input.String())); err != nil {
		t.Fatal(err)
	}
	screen, err := tcell.NewTerminfoScreenFromTty(vt.NewMockTerm(vt.MockOptSize{X: 20, Y: 4}), tcell.OptNegotiation(false))
	if err != nil {
		t.Fatal(err)
	}
	recorder := &recordingRenderer{ui.NewTerminal(screen), make(chan ui.Frame, 256)}
	gate := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(gate) }) }
	application.Renderer = &gatedBottomRenderer{recordingRenderer: recorder, gate: gate, first: true}
	inputPipe, writer := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	result, stopped := make(chan error, 1), make(chan struct{})
	go func() {
		defer close(stopped)
		result <- application.run(ctx, inputPipe, "styled", screen)
	}()
	t.Cleanup(func() {
		cancel()
		release()
		writer.Close()
		select {
		case <-stopped:
		case <-time.After(3 * time.Second):
			t.Error("bottom-jump viewer did not stop")
		}
	})
	frame := waitFrame(t, recorder.frames, firstFrameLine("row000000"))
	if !strings.Contains(frame.Status, "indexing") {
		t.Fatal("test input fully indexed before jump; did not exercise deferred G")
	}
	return testViewer{screen: screen, frames: recorder.frames, result: result, release: release}, writer
}

func TestRunBottomPaintsOnlyDestination(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	for _, binding := range []string{"G", "<End>", ":bottom", "rebound"} {
		t.Run(binding, func(t *testing.T) {
			options := Options{}
			if binding == "rebound" {
				options.Keybinds = map[string][]string{"bottom": {"b"}}
			}
			tail := "TAIL1\nTAIL2\nTAIL3\n"
			if binding == "G" {
				tail = strings.TrimSuffix(tail, "\n") // Live, unterminated final line.
			}
			viewer, writer := startBottomViewer(t, options, tail)
			origin := "row000000"
			if binding == "rebound" {
				viewer.press("j")
				waitFrame(t, viewer.frames, firstFrameLine("row000001"))
				origin = "row000001"
			}
			switch binding {
			case "<End>":
				viewer.press("<End>")
			case ":bottom":
				viewer.press(":", "bottom", "<Enter>")
			case "rebound":
				viewer.press("b")
			default:
				viewer.press("G")
			}
			destination := waitFrame(t, viewer.frames, func(frame ui.Frame) bool {
				if len(frame.Lines) == 0 {
					return false
				}
				if frame.Lines[0] != origin && frame.Lines[0] != "TAIL1" {
					t.Fatalf("bottom jump painted intermediate screen: %q", frame.Lines)
				}
				return firstFrameLine("TAIL1")(frame)
			})
			// G is a jump, not follow: later bytes do not move the destination.
			if _, err := io.WriteString(writer, "\nAFTER1\nAFTER2\nAFTER3\n"); err != nil {
				t.Fatal(err)
			}
			frame := waitFrame(t, viewer.frames, func(frame ui.Frame) bool {
				return !strings.Contains(frame.Status, "indexing") && frame.Status != destination.Status
			})
			if !firstFrameLine("TAIL1")(frame) || strings.Contains(frame.Status, "FOLLOW") {
				t.Fatalf("G followed later stream input: %+v", frame)
			}
			viewer.press("q")
			viewer.expectExit(t)
		})
	}
}

func TestRunPendingBottomUsesFinalFooterHeight(t *testing.T) {
	viewer, _ := startBottomViewer(t, Options{CmdHeight: ui.CmdHeightToggle}, "TAIL0\nTAIL1\nTAIL2\nTAIL3\n")
	viewer.press("=")
	waitFrame(t, viewer.frames, func(frame ui.Frame) bool { return frame.Layout.StatusRow == -1 })
	viewer.press("G", "=") // Shrink back to three body rows while G is pending.
	waitFrame(t, viewer.frames, func(frame ui.Frame) bool {
		if !firstFrameLine("row000000")(frame) && !firstFrameLine("TAIL1")(frame) {
			t.Fatalf("footer resize changed G destination: %+v", frame)
		}
		return firstFrameLine("TAIL1")(frame) && frame.Layout.StatusRow == 3 && len(frame.Lines) == 3
	})
	viewer.press("q")
	viewer.expectExit(t)
}

func TestRunPendingBottomTargetsSnapshotWhileStreamGrows(t *testing.T) {
	viewer, writer := startBottomViewer(t, Options{}, "TAIL1\nTAIL2\nTAIL3\n")
	viewer.press("G")
	waitFrame(t, viewer.frames, func(frame ui.Frame) bool {
		if !firstFrameLine("row000000")(frame) {
			t.Fatalf("G moved viewport before snapshot was indexed: %q", frame.Lines)
		}
		return strings.Contains(frame.Status, "seeking end")
	})
	if _, err := io.WriteString(writer, strings.Repeat("LATER\n", 50000)); err != nil {
		t.Fatal(err)
	}
	waitFrame(t, viewer.frames, func(frame ui.Frame) bool {
		if !firstFrameLine("row000000")(frame) && !firstFrameLine("TAIL1")(frame) {
			t.Fatalf("G chased newly appended input: %q", frame.Lines)
		}
		return firstFrameLine("TAIL1")(frame) && !strings.Contains(frame.Status, "seeking end")
	})
	viewer.press("q")
	viewer.expectExit(t)
}

func TestRunPendingBottomCanBeCanceled(t *testing.T) {
	for _, key := range []string{"<Esc>", "j", "q"} {
		t.Run(key, func(t *testing.T) {
			viewer, _ := startBottomViewer(t, Options{}, "TAIL1\nTAIL2\nTAIL3\n")
			viewer.press("G", key)
			if key == "q" {
				viewer.expectExit(t)
				return
			}
			want := "row000000"
			if key == "j" {
				want = "row000001"
			}
			waitFrame(t, viewer.frames, func(frame ui.Frame) bool {
				if len(frame.Lines) > 0 && frame.Lines[0] != "row000000" && frame.Lines[0] != want {
					t.Fatalf("canceled G moved viewport: %q", frame.Lines)
				}
				return firstFrameLine(want)(frame) && !strings.Contains(frame.Status, "indexing")
			})
			viewer.press("q")
			viewer.expectExit(t)
		})
	}
}
