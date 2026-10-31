package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/araaha/glimpse/internal/ui"
)

func openFollowed(t *testing.T, content string) (string, *os.File) {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "app.log")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return path, file
}

func writeLog(t *testing.T, path, text string, flag int) {
	t.Helper()
	file, err := os.OpenFile(path, flag|os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.WriteString(text); err != nil {
		t.Fatal(err)
	}
}

func TestRunFollowsFileAppendsAndTruncation(t *testing.T) {
	path, file := openFollowed(t, "one\ntwo\n")
	viewer := startReaderViewer(t, Options{CmdHeight: ui.CmdHeightWithSearch, Follow: true}, file, path, 20, 5, false)
	writeLog(t, path, "three\nfour\nfive\n", os.O_APPEND)
	// Following keeps the newest lines in the three-row body.
	frame := waitFrame(t, viewer.frames, func(frame ui.Frame) bool {
		return len(frame.Lines) == 3 && frame.Lines[2] == "five" && frame.StatusState.TotalLinesKnown
	})
	if frame.StatusState.TotalLines != 5 {
		t.Fatalf("total lines = %d", frame.StatusState.TotalLines)
	}
	viewer.press("/")
	viewer.typeText("two")
	viewer.press("<Enter>")
	waitFrame(t, viewer.frames, firstFrameLine("two"))
	writeLog(t, path, "fresh\n", os.O_TRUNC)
	frame = waitFrame(t, viewer.frames, func(frame ui.Frame) bool {
		return strings.HasSuffix(frame.Search, "truncated; reloaded") && len(frame.Lines) == 1 && frame.Lines[0] == "fresh"
	})
	if frame.StatusState.TopLine != 1 || frame.StatusState.SearchCurrent != 0 {
		t.Fatalf("reloaded state = %+v", frame.StatusState)
	}
	viewer.press("q")
	viewer.expectExit(t)
}

func TestRunFollowNameReopensRotatedFile(t *testing.T) {
	path, file := openFollowed(t, "old\n")
	// Mode 0 adds the message row on demand, so the reload notice must
	// appear in the same frame as the reloaded content.
	viewer := startReaderViewer(t, Options{FollowName: true}, file, path, 60, 5, false)
	if err := os.Rename(path, path+".1"); err != nil {
		t.Fatal(err)
	}
	waitFrame(t, viewer.frames, func(frame ui.Frame) bool {
		return strings.Contains(frame.Search, "waiting for it to return") && frame.Lines[0] == "old"
	})
	writeLog(t, path, "rotated\n", 0)
	waitFrame(t, viewer.frames, func(frame ui.Frame) bool {
		return strings.HasSuffix(frame.Search, "replaced; reloaded") && frame.Layout.SearchRow >= 0 && frame.Lines[0] == "rotated"
	})
	viewer.press("q")
	viewer.expectExit(t)
}

func TestRunLabelNamesInputButWatchUsesPath(t *testing.T) {
	path, file := openFollowed(t, "old\n")
	viewer := startReaderViewer(t, Options{FollowName: true, Label: "service log"}, file, path, 60, 5, false)
	writeLog(t, path, "new\n", os.O_TRUNC)
	frame := waitFrame(t, viewer.frames, func(frame ui.Frame) bool { return frame.Lines[0] == "new" })
	if frame.StatusState.Filename != "service log" || frame.Search != "service log truncated; reloaded" {
		t.Fatalf("label = %q, message %q", frame.StatusState.Filename, frame.Search)
	}
	viewer.press("q")
	viewer.expectExit(t)
}

func TestRunWatchesByDescriptorByDefault(t *testing.T) {
	path, file := openFollowed(t, "old\n")
	viewer := startReaderViewer(t, Options{CmdHeight: ui.CmdHeightWithSearch}, file, path, 60, 5, false)
	if err := os.Rename(path, path+".1"); err != nil {
		t.Fatal(err)
	}
	writeLog(t, path, "other\n", 0)
	writeLog(t, path+".1", "late\n", os.O_APPEND)
	frame := waitFrame(t, viewer.frames, func(frame ui.Frame) bool { return len(frame.Lines) == 2 })
	if frame.Lines[1] != "late" || frame.Search != "" {
		t.Fatalf("descriptor watch = %q %q", frame.Lines, frame.Search)
	}
	viewer.press("q")
	viewer.expectExit(t)
}

func TestRunPromptsKeepFollowing(t *testing.T) {
	path, file := openFollowed(t, "one\ntwo\nthree\nfour\nfive\n")
	viewer := startReaderViewer(t, Options{CmdHeight: ui.CmdHeightWithSearch, Follow: true, IncrementalSearch: true}, file, path, 20, 5, false)
	following := func(last string) {
		t.Helper()
		waitFrame(t, viewer.frames, func(frame ui.Frame) bool {
			return frame.Follow && !frame.SearchActive && len(frame.Lines) == 3 && frame.Lines[2] == last
		})
	}
	// A search that finds nothing leaves the view, and following, alone.
	viewer.press("/")
	viewer.typeText("missing")
	viewer.press("<Enter>")
	waitFrame(t, viewer.frames, func(frame ui.Frame) bool { return frame.Search == "Pattern not found: missing" })
	writeLog(t, path, "six\n", os.O_APPEND)
	following("six")
	// Escape undoes an incremental match and resumes following.
	viewer.press("/")
	viewer.typeText("two")
	waitFrame(t, viewer.frames, func(frame ui.Frame) bool { return !frame.Follow && firstFrameLine("two")(frame) })
	viewer.press("<Esc>")
	following("six")
	// An accepted match stops following.
	viewer.press("/")
	viewer.typeText("two")
	viewer.press("<Enter>")
	frame := waitFrame(t, viewer.frames, func(frame ui.Frame) bool { return !frame.SearchActive && firstFrameLine("two")(frame) })
	if frame.Follow {
		t.Fatal("a search match kept following")
	}
	viewer.press("q")
	viewer.expectExit(t)
}
