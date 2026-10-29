package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"glimpse/internal/keybind"
	"glimpse/internal/pager"
	"glimpse/internal/search"
	"glimpse/internal/ui"

	"github.com/gdamore/tcell/v3"
	"github.com/gdamore/tcell/v3/vt"
)

// unsuspendedScreen keeps the mock terminal running while foreground commands
// run: the vt mock restarts its emulator on Resume without synchronizing with
// tcell's input goroutine, a race in the mock alone.
type unsuspendedScreen struct{ tcell.Screen }

func (unsuspendedScreen) Suspend() error { return nil }
func (unsuspendedScreen) Resume() error  { return nil }

type testViewer struct {
	screen  tcell.Screen
	frames  <-chan ui.Frame
	result  <-chan error
	release func()
}

func startTestViewer(t *testing.T, options Options, input string, width, height int) testViewer {
	t.Helper()
	return startNamedTestViewer(t, options, input, "test", width, height)
}

func startNamedTestViewer(t *testing.T, options Options, input, name string, width, height int) testViewer {
	t.Helper()
	return startReaderViewer(t, options, io.NopCloser(strings.NewReader(input)), name, width, height, input == "")
}

// startReaderViewer runs the viewer on any input, such as a real file, and
// waits for its first complete frame.
func startReaderViewer(t *testing.T, options Options, reader io.ReadCloser, name string, width, height int, empty bool) testViewer {
	t.Helper()
	application, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	mock, err := tcell.NewTerminfoScreenFromTty(vt.NewMockTerm(vt.MockOptSize{X: vt.Col(width), Y: vt.Row(height)}), tcell.OptNegotiation(false))
	if err != nil {
		t.Fatal(err)
	}
	screen := unsuspendedScreen{mock}
	recorder := &recordingRenderer{ui.NewTerminal(screen), make(chan ui.Frame, 32)}
	application.Renderer = recorder
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		result <- application.run(ctx, reader, name, screen)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-stopped:
		case <-time.After(3 * time.Second):
			t.Error("viewer did not stop")
		}
	})
	waitFrame(t, recorder.frames, func(frame ui.Frame) bool {
		return (len(frame.Lines) > 0 || empty) && !frame.Reading
	})
	return testViewer{screen: screen, frames: recorder.frames, result: result}
}

func (v testViewer) press(keys ...string) {
	if v.release != nil {
		defer v.release()
	}
	for _, key := range keys {
		code, named := map[string]tcell.Key{
			"<Esc>": tcell.KeyEsc, "<Enter>": tcell.KeyEnter, "<Up>": tcell.KeyUp, "<Down>": tcell.KeyDown,
			"<Left>": tcell.KeyLeft, "<Right>": tcell.KeyRight, "<Backspace>": tcell.KeyBackspace2,
			"<End>": tcell.KeyEnd, "<Tab>": tcell.KeyTab, "<S-Tab>": tcell.KeyBacktab,
			"<C-w>": tcell.KeyCtrlW, "<C-a>": tcell.KeyCtrlA, "<C-e>": tcell.KeyCtrlE, "<C-u>": tcell.KeyCtrlU,
		}[key]
		if named {
			v.screen.EventQ() <- tcell.NewEventKey(code, "", tcell.ModNone)
			continue
		}
		if strings.HasPrefix(key, "<M-") {
			v.screen.EventQ() <- tcell.NewEventKey(tcell.KeyRune, key[3:len(key)-1], tcell.ModAlt)
			continue
		}
		v.screen.EventQ() <- tcell.NewEventKey(tcell.KeyRune, key, tcell.ModNone)
	}
}

func (v testViewer) typeText(text string) {
	for _, r := range text {
		v.press(string(r))
	}
}

func searchFrame(text string, active bool) func(ui.Frame) bool {
	return func(frame ui.Frame) bool { return frame.Search == text && frame.SearchActive == active }
}

func TestRunSearchAndRepeat(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	viewer := startTestViewer(t, Options{SmartCase: true}, "start\nabcdefghNEEDLEsuffix\ngap\nneedle end\nfinish", 8, 3)
	viewer.press("/")
	waitFrame(t, viewer.frames, searchFrame("/", true))
	viewer.typeText("needle")
	viewer.press("<Enter>")
	frame := waitFrame(t, viewer.frames, searchFrame("/needle", false))
	if !firstFrameLine("NEEDLEsu")(frame) {
		t.Fatalf("search didn't jump to wrapped match row: %q", frame.Lines)
	}
	viewer.press("n")
	frame = waitFrame(t, viewer.frames, searchFrame("/needle", false))
	if !firstFrameLine("needle e")(frame) {
		t.Fatalf("next = %q", frame.Lines)
	}
	viewer.press("n")
	frame = waitFrame(t, viewer.frames, searchFrame("/needle  [wrapped]", false))
	if !firstFrameLine("NEEDLEsu")(frame) {
		t.Fatalf("wrap = %q", frame.Lines)
	}
	viewer.press("N")
	frame = waitFrame(t, viewer.frames, searchFrame("/needle  [wrapped]", false))
	if !firstFrameLine("needle e")(frame) {
		t.Fatalf("opposite direction = %q", frame.Lines)
	}
	viewer.press("?")
	viewer.typeText("NEEDLE")
	viewer.press("<Enter>")
	frame = waitFrame(t, viewer.frames, searchFrame("?NEEDLE", false))
	if !firstFrameLine("NEEDLEsu")(frame) {
		t.Fatalf("backward smart-case = %q", frame.Lines)
	}
	viewer.press("q")
	viewer.expectExit(t)
}

func TestRunSearchEditingHistoryAndRebinding(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	options := Options{
		Keybinds: map[string][]string{
			"search-forward": {"s", "s"}, "search-accept": {"<M-s>"},
			"search-word-left": {"<M-h>"}, "search-word-right": {"<M-l>"},
		},
	}
	viewer := startTestViewer(t, options, "start\none two\nnext\nlast", 12, 3)
	viewer.press("s", "s")
	viewer.typeText("one three")
	viewer.press("<C-w>")
	waitFrame(t, viewer.frames, searchFrame("/one ", true))
	viewer.typeText("two")
	viewer.press("<M-h>")
	frame := waitFrame(t, viewer.frames, searchFrame("/one two", true))
	if frame.SearchCursor != 5 {
		t.Fatalf("word-left cursor = %d", frame.SearchCursor)
	}
	viewer.press("<M-l>")
	frame = waitFrame(t, viewer.frames, func(frame ui.Frame) bool { return frame.SearchActive && frame.SearchCursor == 8 })
	viewer.press("<M-s>")
	waitFrame(t, viewer.frames, searchFrame("/one two", false))
	viewer.press("q")
	viewer.expectExit(t)
	// A new viewer loads persistent history and restores the draft with Down.
	viewer = startTestViewer(t, options, "start\none two\nnext\nlast", 12, 3)
	viewer.press("s", "s")
	viewer.typeText("draft")
	viewer.press("<Up>")
	waitFrame(t, viewer.frames, searchFrame("/one two", true))
	viewer.press("<Down>")
	waitFrame(t, viewer.frames, searchFrame("/draft", true))
	viewer.press("<Esc>")
	frame = waitFrame(t, viewer.frames, func(frame ui.Frame) bool { return !frame.SearchActive && frame.Layout.SearchRow == -1 })
	if !firstFrameLine("start")(frame) {
		t.Fatalf("cancel moved the view: %q", frame.Lines)
	}
	viewer.press("q")
	viewer.expectExit(t)
}

func TestRunSearchMessagesAndEmptyReuse(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	viewer := startTestViewer(t, Options{CmdHeight: ui.CmdHeightWithSearch}, "start\ntarget\nend", 20, 4)
	viewer.press("n")
	waitFrame(t, viewer.frames, searchFrame("No previous search", false))
	viewer.press("/")
	viewer.typeText("absent")
	viewer.press("<Enter>")
	waitFrame(t, viewer.frames, searchFrame("Pattern not found: absent", false))
	viewer.press("/")
	viewer.typeText("target")
	viewer.press("<Enter>")
	waitFrame(t, viewer.frames, searchFrame("/target", false))
	viewer.press("/", "<Enter>")
	frame := waitFrame(t, viewer.frames, searchFrame("/target", false))
	if frame.Layout.StatusRow < 0 || frame.Layout.SearchRow < 0 {
		t.Fatalf("mode 1 lost a footer: %+v", frame.Layout)
	}
	viewer.press("q")
	viewer.expectExit(t)
}

func TestRunSearchModeSwitchRetriesKeysInNewMode(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	viewer := startTestViewer(t, Options{
		Keybinds: map[string][]string{
			"search-forward": {"s"}, "up": {"s", "x", "x"},
			"search-cancel": {"g"}, "search-left": {"g", "g"},
		},
	}, "one\ntwo\nthree", 12, 2)
	// s is a completed search binding, but also a prefix of sxx. The suffix
	// xq must enter the prompt literally, not be swallowed or quit the pager.
	viewer.press("s", "x", "q")
	waitFrame(t, viewer.frames, searchFrame("/xq", true))
	// A mismatching q after an ambiguous prompt cancellation is a fresh normal
	// mode quit, just as cancellation preserves commands in the main matcher.
	viewer.press("g", "q")
	viewer.expectExit(t)
}

func TestRunRapidSearchRepeatsAreNotDropped(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	viewer := startTestViewer(t, Options{}, "first hit\ngap\nsecond hit\ngap\nthird hit\ngap\nfourth hit", 20, 3)
	viewer.press("/")
	viewer.typeText("hit")
	viewer.press("<Enter>", "n", "n")
	frame := waitFrame(t, viewer.frames, func(frame ui.Frame) bool {
		return searchFrame("/hit", false)(frame) && firstFrameLine("third hit")(frame)
	})
	if frame.SearchActive {
		t.Fatal("search remained in input mode")
	}
	viewer.press("n", "N")
	waitFrame(t, viewer.frames, func(frame ui.Frame) bool {
		return searchFrame("/hit", false)(frame) && firstFrameLine("third hit")(frame)
	})
	viewer.press("q")
	viewer.expectExit(t)
}

func TestRunIncrementalSearchRestoresAnchor(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	viewer := startTestViewer(t, Options{IncrementalSearch: true}, "start\none\ntwo\ntarget\nend", 12, 3)
	viewer.press("/")
	viewer.typeText("target")
	frame := waitFrame(t, viewer.frames, func(frame ui.Frame) bool { return frame.SearchActive && firstFrameLine("target")(frame) })
	if len(frame.Highlights) == 0 {
		t.Fatal("preview not highlighted")
	}
	viewer.press("<Esc>")
	waitFrame(t, viewer.frames, func(frame ui.Frame) bool { return !frame.SearchActive && firstFrameLine("start")(frame) })
	viewer.press("n")
	waitFrame(t, viewer.frames, searchFrame("No previous search", false))
	viewer.press("q")
	viewer.expectExit(t)
}

func TestRunCommandPrompt(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	viewer := startTestViewer(t, Options{}, "one\ntwo\nthree\nfour", 12, 2)
	viewer.press(":")
	viewer.typeText("3")
	viewer.press("<Enter>")
	waitFrame(t, viewer.frames, firstFrameLine("three"))
	viewer.press(":")
	viewer.typeText("q")
	viewer.press("<Enter>")
	viewer.expectExit(t)
}

func TestRunPasteDoesNotExecuteBindings(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	viewer := startTestViewer(t, Options{}, "one\ntwo\nthree", 12, 2)
	viewer.screen.EventQ() <- tcell.NewEventPaste(true)
	viewer.typeText("q/G")
	viewer.screen.EventQ() <- tcell.NewEventPaste(false)
	viewer.press("/")
	viewer.screen.EventQ() <- tcell.NewEventPaste(true)
	viewer.typeText("q/界")
	viewer.press("<Enter>")
	viewer.screen.EventQ() <- tcell.NewEventPaste(false)
	waitFrame(t, viewer.frames, searchFrame("/q/界", true))
	viewer.press("<Esc>", "q")
	viewer.expectExit(t)
}

func TestRunBackwardPreviewAcceptUsesOriginalOrigin(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	viewer := startTestViewer(t, Options{IncrementalSearch: true}, "first hit\ngap\nsecond hit\nlast\nend", 20, 3)
	viewer.press("G")
	waitFrame(t, viewer.frames, firstFrameLine("last"))
	viewer.press("?")
	viewer.typeText("hit")
	waitFrame(t, viewer.frames, func(frame ui.Frame) bool { return frame.SearchActive && firstFrameLine("second hit")(frame) })
	viewer.press("<Enter>")
	frame := waitFrame(t, viewer.frames, searchFrame("?hit", false))
	if !firstFrameLine("second hit")(frame) {
		t.Fatalf("accept skipped preview: %q", frame.Lines)
	}
	viewer.press("q")
	viewer.expectExit(t)
}

func TestRunCommandNavigationClearsSearchAnchor(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	viewer := startTestViewer(t, Options{}, "first hit\ngap\nsecond hit\ngap\nthird hit\nend", 20, 3)
	viewer.press("/")
	viewer.typeText("hit")
	viewer.press("<Enter>")
	waitFrame(t, viewer.frames, searchFrame("/hit", false))
	viewer.press("n")
	waitFrame(t, viewer.frames, firstFrameLine("second hit"))
	viewer.press(":")
	viewer.typeText("top")
	viewer.press("<Enter>")
	waitFrame(t, viewer.frames, firstFrameLine("first hit"))
	viewer.press("n")
	frame := waitFrame(t, viewer.frames, searchFrame("/hit", false))
	if !firstFrameLine("first hit")(frame) {
		t.Fatalf("command left stale search anchor: %q", frame.Lines)
	}
	viewer.press("q")
	viewer.expectExit(t)
}

func TestRunPasteSettlesPendingLiteralPrefix(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	viewer := startTestViewer(t, Options{Keybinds: map[string][]string{"search-left": {"j", "j"}, "search-accept": {"j"}}}, "one\ntwo", 12, 2)
	viewer.press("/", "j")
	viewer.screen.EventQ() <- tcell.NewEventPaste(true)
	viewer.typeText("abc")
	viewer.screen.EventQ() <- tcell.NewEventPaste(false)
	waitFrame(t, viewer.frames, searchFrame("/jabc", true))
	viewer.press("<Esc>", "q")
	viewer.expectExit(t)
}

func TestRunQuitUnblocksStorageQuota(t *testing.T) {
	application, err := New(Options{ChunkSize: 32, Storage: pager.StorageOptions{MemoryBudget: 128, SpoolQuota: 256, SpoolDir: t.TempDir()}})
	if err != nil {
		t.Fatal(err)
	}
	screen, err := tcell.NewTerminfoScreenFromTty(vt.NewMockTerm(vt.MockOptSize{X: 80, Y: 4}), tcell.OptNegotiation(false))
	if err != nil {
		t.Fatal(err)
	}
	recorder := &recordingRenderer{ui.NewTerminal(screen), make(chan ui.Frame, 32)}
	application.Renderer = recorder
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- application.run(ctx, io.NopCloser(strings.NewReader(strings.Repeat("x", 300))), "test", screen)
	}()
	waitFrame(t, recorder.frames, func(frame ui.Frame) bool { return strings.HasPrefix(frame.Search, "Storage paused:") })
	viewer := testViewer{screen: screen, frames: recorder.frames, result: result}
	viewer.press("q")
	viewer.expectExit(t)
}

func TestRunQuotaCommandResumesIngestion(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	application, _ := New(Options{ChunkSize: 32, Storage: pager.StorageOptions{MemoryBudget: 128, SpoolQuota: 256, SpoolDir: t.TempDir()}})
	screen, err := tcell.NewTerminfoScreenFromTty(vt.NewMockTerm(vt.MockOptSize{X: 80, Y: 4}), tcell.OptNegotiation(false))
	if err != nil {
		t.Fatal(err)
	}
	recorder := &recordingRenderer{ui.NewTerminal(screen), make(chan ui.Frame, 32)}
	application.Renderer = recorder
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- application.run(ctx, io.NopCloser(strings.NewReader(strings.Repeat("x", 300))), "test", screen)
	}()
	waitFrame(t, recorder.frames, func(frame ui.Frame) bool { return strings.HasPrefix(frame.Search, "Storage paused:") })
	viewer := testViewer{screen: screen, frames: recorder.frames, result: result}
	viewer.press(":")
	viewer.typeText("set spool-quota=512")
	viewer.press("<Enter>")
	waitFrame(t, viewer.frames, func(frame ui.Frame) bool {
		return frame.Search == "spool-quota=512" && !frame.Reading
	})
	viewer.press("q")
	viewer.expectExit(t)
}

func TestRunReportsSpoolCleanupFailure(t *testing.T) {
	dir := t.TempDir()
	viewer := startTestViewer(t, Options{ChunkSize: 32, Storage: pager.StorageOptions{MemoryBudget: 128, SpoolQuota: 1024, SpoolDir: dir}}, strings.Repeat("x", 300), 20, 3)
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("spool files = %v", entries)
	}
	path := filepath.Join(dir, entries[0].Name())
	if err := os.Rename(path, path+"-moved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "child"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	viewer.press("q")
	select {
	case err := <-viewer.result:
		if err == nil {
			t.Fatal("cleanup failure silently ignored")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cleanup did not finish")
	}
}

func TestSearchResponseRejectsEvictedOffset(t *testing.T) {
	response := searchResponse{result: search.Result{Offset: 10, Found: true}}
	if response.available(20, 100) || response.available(0, 10) || !response.available(10, 100) {
		t.Fatal("retained match range check failed")
	}
}

func TestRunRollingNoticeSurvivesSearchMessages(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	viewer := startTestViewer(t, Options{Storage: pager.StorageOptions{MemoryBudget: 64, SpoolQuota: 128, Retention: "rolling"}}, strings.Repeat("hit\n", 32), 80, 3)
	viewer.press("/")
	viewer.typeText("hit")
	viewer.press("<Enter>")
	waitFrame(t, viewer.frames, func(frame ui.Frame) bool {
		return strings.HasPrefix(frame.Search, "/hit") && strings.Contains(frame.Search, "64 bytes discarded")
	})
	viewer.press("q")
	viewer.expectExit(t)
}

func TestRunSearchAcceptsComposedTerminalEvents(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	viewer := startTestViewer(t, Options{}, "start\n👩🏽‍💻\nend", 10, 2)
	viewer.press("/", "👩🏽‍💻")
	waitFrame(t, viewer.frames, searchFrame("/👩🏽‍💻", true))
	viewer.press("<Backspace>")
	waitFrame(t, viewer.frames, searchFrame("/", true))
	viewer.press("<Esc>", "q")
	viewer.expectExit(t)
}

func TestRunComposedInsertionPreservesExistingSuffixOrder(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	for _, tt := range []struct{ suffix, insertion string }{{"🦊", "👩‍💻"}, {"🇨", "🇺🇳"}} {
		viewer := startTestViewer(t, Options{}, "one\ntwo", 20, 2)
		viewer.press("/", tt.suffix, "<C-a>", tt.insertion)
		waitFrame(t, viewer.frames, searchFrame("/"+tt.insertion+tt.suffix, true))
		viewer.press("<Esc>", "q")
		viewer.expectExit(t)
	}
}

func TestRunLargeFilePaintsAndQuitsBeforeFullIndex(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "large-input")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("header\n"); err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(256 << 20); err != nil {
		t.Fatal(err)
	}
	file.Seek(0, io.SeekStart)
	application, _ := New(Options{})
	screen, err := tcell.NewTerminfoScreenFromTty(vt.NewMockTerm(vt.MockOptSize{X: 80, Y: 24}), tcell.OptNegotiation(false))
	if err != nil {
		t.Fatal(err)
	}
	recorder := &recordingRenderer{ui.NewTerminal(screen), make(chan ui.Frame, 32)}
	application.Renderer = recorder
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	stopped := make(chan struct{})
	go func() { defer close(stopped); result <- application.run(ctx, file, "large", screen) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-stopped:
		case <-time.After(3 * time.Second):
			t.Error("large-file viewer did not stop")
		}
	})
	frame := waitFrame(t, recorder.frames, firstFrameLine("header"))
	if !frame.Indexing {
		t.Fatal("large file was completely indexed before first visible frame")
	}
	viewer := testViewer{screen: screen, frames: recorder.frames, result: result}
	viewer.press("q")
	viewer.expectExit(t)
}

func TestRunFollowCatchesUpWithNewlineFreeBurst(t *testing.T) {
	application, _ := New(Options{Follow: true})
	screen, err := tcell.NewTerminfoScreenFromTty(vt.NewMockTerm(vt.MockOptSize{X: 80, Y: 24}), tcell.OptNegotiation(false))
	if err != nil {
		t.Fatal(err)
	}
	recorder := &recordingRenderer{ui.NewTerminal(screen), make(chan ui.Frame, 64)}
	application.Renderer = recorder
	input, writer := io.Pipe()
	defer writer.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	stopped := make(chan struct{})
	go func() { defer close(stopped); result <- application.run(ctx, input, "stream", screen) }()
	t.Cleanup(func() {
		cancel()
		writer.Close()
		select {
		case <-stopped:
		case <-time.After(3 * time.Second):
			t.Error("stream viewer did not stop")
		}
	})
	written := make(chan error, 1)
	go func() {
		part := []byte(strings.Repeat("a", 64<<10))
		for range 512 {
			if _, err := writer.Write(part); err != nil {
				written <- err
				return
			}
		}
		_, err := io.WriteString(writer, "LATEST-TAIL-MARKER")
		written <- err
	}()
	waitFrame(t, recorder.frames, func(frame ui.Frame) bool {
		return strings.Contains(strings.Join(frame.Lines, ""), "LATEST-TAIL-MARKER") && frame.Follow
	})
	if err := <-written; err != nil {
		t.Fatal(err)
	}
	viewer := testViewer{screen: screen, frames: recorder.frames, result: result}
	viewer.press("q")
	viewer.expectExit(t)
}

func TestRunSearchHistoryFailureIsNonfatal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_DATA_HOME", path)
	viewer := startTestViewer(t, Options{}, "start\ntarget\nend", 30, 3)
	viewer.press("/")
	viewer.typeText("target")
	viewer.press("<Enter>")
	frame := waitFrame(t, viewer.frames, func(frame ui.Frame) bool {
		return !frame.SearchActive && strings.HasPrefix(frame.Search, "/target  [history:")
	})
	if !firstFrameLine("target")(frame) {
		t.Fatalf("history failure blocked search: %q", frame.Lines)
	}
	viewer.press("q")
	viewer.expectExit(t)
}

func (v testViewer) expectExit(t *testing.T) {
	t.Helper()
	select {
	case err := <-v.result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("viewer did not quit")
	}
}

func firstFrameLine(line string) func(ui.Frame) bool {
	return func(frame ui.Frame) bool { return len(frame.Lines) > 0 && frame.Lines[0] == line }
}

func TestRunCancelFallbackPreservesFollowingQuit(t *testing.T) {
	viewer := startTestViewer(t, Options{Keybinds: map[string][]string{"cancel": {"g"}}}, "one\ntwo\nthree\nfour", 8, 2)
	viewer.press("g", "q")
	viewer.expectExit(t)
}

func TestRunCancelFallbackPreservesFollowingPrefix(t *testing.T) {
	viewer := startTestViewer(t, Options{Keybinds: map[string][]string{
		"cancel": {"g"}, "quit": {"z", "q"},
	}}, "one\ntwo\nthree\nfour", 8, 2)
	viewer.press("g", "z", "q")
	viewer.expectExit(t)
}

func TestRunCancelDiscardsShorterAmbiguousAction(t *testing.T) {
	for _, cancelKey := range []string{"<Esc>", "x"} {
		t.Run(cancelKey, func(t *testing.T) {
			viewer := startTestViewer(t, Options{Keybinds: map[string][]string{
				"up": {"g"}, "cancel": {cancelKey},
			}}, "one\ntwo\nthree\nfour", 8, 2)
			viewer.press("j")
			waitFrame(t, viewer.frames, firstFrameLine("two"))
			viewer.press("g", cancelKey)
			viewer.screen.EventQ() <- tcell.NewEventResize(8, 2)
			frame := waitFrame(t, viewer.frames, func(ui.Frame) bool { return true })
			if !firstFrameLine("two")(frame) {
				t.Fatalf("cancellation executed the shorter up action: %q", frame.Lines)
			}
			viewer.press("q")
			viewer.expectExit(t)
		})
	}
}

func TestRunAmbiguousBindingTimesOut(t *testing.T) {
	viewer := startTestViewer(t, Options{Keybinds: map[string][]string{"up": {"g"}}}, "one\ntwo\nthree\nfour", 8, 3)
	viewer.press("G")
	waitFrame(t, viewer.frames, firstFrameLine("three"))
	viewer.press("g")
	waitFrame(t, viewer.frames, firstFrameLine("two"))
	viewer.press("q")
	viewer.expectExit(t)
}

func TestRunCountedGotoLogicalLine(t *testing.T) {
	var input strings.Builder
	for line := 1; line <= 60; line++ {
		fmt.Fprintf(&input, "line%02d-wrapped\n", line)
	}
	for _, tt := range []struct {
		name    string
		options Options
		binding []string
	}{
		{"default", Options{Follow: true}, []string{"G"}},
		{"rebound", Options{Keybinds: map[string][]string{"bottom": {"z", "z"}}}, []string{"z", "z"}},
		{"ambiguous", Options{Keybinds: map[string][]string{"bottom": {"g"}}}, []string{"g"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			viewer := startTestViewer(t, tt.options, input.String(), 8, 5)
			viewer.press("5", "0")
			viewer.press(tt.binding...)
			frame := waitFrame(t, viewer.frames, firstFrameLine("line50-w"))
			if len(frame.Lines) < 2 || frame.Lines[1] != "rapped" || frame.Follow {
				t.Fatalf("counted jump did not preserve wrapping or leave follow: %+v", frame)
			}
			viewer.press("1")
			viewer.press(tt.binding...)
			waitFrame(t, viewer.frames, firstFrameLine("line01-w"))
			viewer.press("9", "9", "9")
			viewer.press(tt.binding...)
			waitFrame(t, viewer.frames, firstFrameLine("line60-w"))
			// With no count, the original bottom action fills the last screen.
			viewer.press(tt.binding...)
			waitFrame(t, viewer.frames, firstFrameLine("line59-w"))
			viewer.press("5", "<Esc>")
			viewer.press(tt.binding...)
			waitFrame(t, viewer.frames, firstFrameLine("line59-w"))
			viewer.press("q")
			viewer.expectExit(t)
		})
	}
}

func TestRunAmbiguousBindingPrefersLongerSequence(t *testing.T) {
	viewer := startTestViewer(t, Options{Keybinds: map[string][]string{"up": {"g"}}}, "one\ntwo\nthree\nfour\nfive\nsix", 8, 3)
	viewer.press("G")
	waitFrame(t, viewer.frames, firstFrameLine("five"))
	viewer.press("g", "g")
	waitFrame(t, viewer.frames, firstFrameLine("one"))
	viewer.press("q")
	viewer.expectExit(t)
}

func TestRunFollowAndNavigationInSameActionBatch(t *testing.T) {
	var input strings.Builder
	for row := 0; row < 20; row++ {
		fmt.Fprintf(&input, "row%02d\n", row)
	}
	viewer := startTestViewer(t, Options{Keybinds: map[string][]string{
		"follow": {"x"}, "top": {"x", "x"},
	}}, input.String(), 8, 5)
	viewer.press("j")
	waitFrame(t, viewer.frames, firstFrameLine("row01"))
	viewer.press("j")
	waitFrame(t, viewer.frames, firstFrameLine("row02"))
	// x followed by k resolves to follow then up in a single key-event batch.
	viewer.press("x", "k")
	waitFrame(t, viewer.frames, firstFrameLine("row15"))
	viewer.press("q")
	viewer.expectExit(t)
}

func TestFollowJumpSurvivesNavigationBeforeRepaint(t *testing.T) {
	for _, tt := range []struct {
		action keybind.Action
		top    int
	}{
		{keybind.Up, 15},
		{keybind.PageUp, 12},
		{keybind.HalfPageUp, 14},
		{keybind.Down, 16},
	} {
		t.Run(string(tt.action), func(t *testing.T) {
			view := viewState{top: 2, height: 4}
			view.apply(keybind.Follow, 20, 4)
			view.apply(tt.action, 20, 4)
			view.reflow(view.top, 20, 4, false)
			if view.top != tt.top || view.follow || view.bottom {
				t.Fatalf("follow then %s before repaint = %+v, want top %d with follow off", tt.action, view, tt.top)
			}
		})
	}
}

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
	application, err := New(Options{CmdHeight: ui.CmdHeightToggle})
	if err != nil {
		t.Fatal(err)
	}
	application.UI.ToggleStatusline() // Exercise streaming from mode -1's hidden state.
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
		result <- application.run(ctx, input, "", screen)
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
	initial := waitFrame(t, recorder.frames, firstLine("abcdefgh"))
	if initial.StatusState.Filename != "" || initial.StatusState.TotalLinesKnown || initial.StatusState.TopLine != 1 {
		t.Fatalf("live stdin status = %+v", initial.StatusState)
	}
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
	application, _ := New(Options{Follow: true, CmdHeight: ui.CmdHeightToggle})
	application.UI.ToggleStatusline()
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
		result <- application.run(ctx, input, "", screen)
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
		return len(frame.Lines) == 3 && frame.Lines[0] == "abcd" && frame.Follow
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
	screen, err := tcell.NewTerminfoScreenFromTty(vt.NewMockTerm(vt.MockOptSize{X: 4, Y: 3}), tcell.OptNegotiation(false))
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

func TestRunRegexSearch(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	viewer := startTestViewer(t, Options{CmdHeight: ui.CmdHeightWithSearch, IncrementalSearch: true}, "one\nfoo1\nbar\nfoo22 end\na.c\nabc\n", 20, 4)
	viewer.press("/")
	viewer.typeText(`^foo\d+`)
	viewer.press("<Enter>")
	waitFrame(t, viewer.frames, firstFrameLine("foo1"))
	viewer.press("n")
	waitFrame(t, viewer.frames, firstFrameLine("foo22 end"))
	// An invalid pattern is explained while typing and neither moves the view
	// nor replaces the previous search.
	viewer.press("/")
	viewer.typeText("a(")
	frame := waitFrame(t, viewer.frames, func(frame ui.Frame) bool { return frame.Search == "/a(" })
	if frame.Ghost != "  invalid pattern: missing closing )" || !firstFrameLine("foo22 end")(frame) {
		t.Fatalf("invalid preview: ghost %q, lines %q", frame.Ghost, frame.Lines)
	}
	viewer.press("<Enter>")
	waitFrame(t, viewer.frames, searchFrame("invalid pattern: missing closing )", false))
	viewer.press("n")
	waitFrame(t, viewer.frames, firstFrameLine("foo1"))
	viewer.press("/")
	viewer.typeText(`\Va.c`)
	viewer.press("<Enter>")
	waitFrame(t, viewer.frames, firstFrameLine("a.c"))
	viewer.press(":")
	viewer.typeText("set nomagic")
	viewer.press("<Enter>")
	waitFrame(t, viewer.frames, searchFrame("nomagic", false))
	viewer.press("/")
	viewer.typeText("^foo")
	viewer.press("<Enter>")
	waitFrame(t, viewer.frames, searchFrame("Pattern not found: ^foo", false))
	viewer.press("q")
	viewer.expectExit(t)
}
