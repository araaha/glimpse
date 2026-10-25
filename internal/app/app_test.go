package app

import (
	"testing"

	"glimpse/internal/ui"
)

func TestNew(t *testing.T) {
	application, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	if application.Document == nil || application.UI == nil {
		t.Fatal("New() did not initialize document and UI state")
	}
	if got := application.UI.Layout(80, 24); got.StatusRow != -1 || got.SearchRow != -1 || got.Body.Height != 24 {
		t.Fatalf("default footer layout = %+v", got)
	}
}

func TestNewUsesCmdHeight(t *testing.T) {
	application, err := New(Options{CmdHeight: ui.CmdHeightWithSearch})
	if err != nil {
		t.Fatal(err)
	}
	if got := application.UI.Layout(80, 24); got.StatusRow != 22 || got.SearchRow != 23 {
		t.Fatalf("configured footer layout = %+v", got)
	}
}

func TestNewRejectsInvalidCmdHeight(t *testing.T) {
	if application, err := New(Options{CmdHeight: 2}); application != nil || err == nil {
		t.Fatalf("New() = (%v, %v), want (nil, error)", application, err)
	}
}
