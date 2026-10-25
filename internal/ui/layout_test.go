package ui

import "testing"

func TestLayout(t *testing.T) {
	tests := []struct {
		name      string
		cmdHeight CmdHeight
		width     int
		height    int
		bodyWidth int
		bodyRows  int
		statusRow int
		searchRow int
	}{
		{"toggle starts visible", CmdHeightToggle, 80, 24, 80, 23, 23, -1},
		{"mode zero starts hidden", CmdHeightStatusOnly, 80, 24, 80, 24, -1, -1},
		{"status above search", CmdHeightWithSearch, 80, 24, 80, 22, 22, 23},
		{"only footer fits", CmdHeightWithSearch, 80, 2, 80, 0, 0, 1},
		{"search takes priority", CmdHeightWithSearch, 80, 1, 80, 0, -1, 0},
		{"one status row fits", CmdHeightToggle, 80, 1, 80, 0, 0, -1},
		{"mode zero keeps single row for body", CmdHeightStatusOnly, 80, 1, 80, 1, -1, -1},
		{"zero height", CmdHeightWithSearch, 80, 0, 80, 0, -1, -1},
		{"negative size", CmdHeightWithSearch, -1, -1, 0, 0, -1, -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model, err := NewModel(tt.cmdHeight)
			if err != nil {
				t.Fatal(err)
			}
			want := Layout{
				Body:      Viewport{Width: tt.bodyWidth, Height: tt.bodyRows},
				StatusRow: tt.statusRow,
				SearchRow: tt.searchRow,
			}
			if got := model.Layout(tt.width, tt.height); got != want {
				t.Fatalf("Layout() = %+v, want %+v", got, want)
			}
		})
	}
}

func TestToggleStatusline(t *testing.T) {
	for _, mode := range []CmdHeight{CmdHeightToggle, CmdHeightStatusOnly, CmdHeightWithSearch} {
		model, err := NewModel(mode)
		if err != nil {
			t.Fatal(err)
		}
		before := model.Layout(80, 24)
		model.ToggleStatusline()
		after := model.Layout(80, 24)
		if mode == CmdHeightStatusOnly {
			if after.StatusRow != 23 || after.SearchRow != -1 || after.Body.Height != 23 {
				t.Fatalf("shown statusline layout = %+v", after)
			}
		} else if mode == CmdHeightToggle {
			if after.StatusRow != -1 || after.SearchRow != -1 || after.Body.Height != 24 {
				t.Fatalf("hidden statusline layout = %+v", after)
			}
		} else if after != before {
			t.Fatalf("ToggleStatusline() changed layout for cmdheight = %d", mode)
		}
		model.ToggleStatusline()
		if got := model.Layout(80, 24); got != before {
			t.Fatalf("toggling twice: got %+v, want %+v", got, before)
		}
	}
}

func TestLayoutAfterResize(t *testing.T) {
	model, err := NewModel(CmdHeightWithSearch)
	if err != nil {
		t.Fatal(err)
	}
	initial := model.Layout(80, 24)
	model.Layout(40, 1)
	if got := model.Layout(80, 24); got != initial {
		t.Fatalf("layout did not recover after resize: got %+v, want %+v", got, initial)
	}
}

func TestNewModelRejectsInvalidCmdHeight(t *testing.T) {
	for _, mode := range []CmdHeight{-2, 2, 100} {
		if model, err := NewModel(mode); model != nil || err == nil {
			t.Fatalf("NewModel(%d) = (%v, %v), want (nil, error)", mode, model, err)
		}
	}
}
