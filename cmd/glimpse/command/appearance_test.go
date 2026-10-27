package command

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"glimpse/internal/app"
	"glimpse/internal/ui"

	"github.com/gdamore/tcell/v3/color"
)

func TestAppearanceConfiguration(t *testing.T) {
	for _, tt := range []struct {
		name, config, wantErr string
		env                   map[string]string
		check                 func(*testing.T, *app.App)
	}{
		{name: "defaults", check: func(t *testing.T, a *app.App) {
			if !a.Statusline.Has(ui.StatusSearchResults) || a.SearchHighlight.GetBackground().Valid() {
				t.Fatal("appearance defaults changed")
			}
		}},
		{name: "selection colors and separator", config: "[statusline]\nleft = ['MODE']\nright = ['LINE_NUMBERS']\nseparator = ' / '\nbg = '#112233'\n[statusline.styles]\nmode = {bg = '#445566', fg = '#abcdef'}\n[search.highlight-style]\nfg = '#010203'\nbg = '#040506'\n", check: func(t *testing.T, a *app.App) {
			if a.Statusline.Has(ui.StatusFilename) || a.Statusline.Has(ui.StatusSearchResults) {
				t.Fatal("disabled sections retained")
			}
			bar := a.Statusline.Format(ui.StatusState{Mode: "VIEW", TopLine: 1, TotalLines: 3, TotalLinesKnown: true}, 30, false)
			if bar.Style.GetBackground() != color.NewHexColor(0x112233) || bar.Spans[0].Style.GetBackground() != color.NewHexColor(0x445566) || bar.Spans[0].Style.GetForeground() != color.NewHexColor(0xabcdef) {
				t.Fatal("configured status colors lost")
			}
			if a.SearchHighlight.GetForeground() != color.NewHexColor(0x010203) || a.SearchHighlight.GetBackground() != color.NewHexColor(0x040506) {
				t.Fatal("configured highlight colors lost")
			}
		}},
		{name: "empty sides", config: "[statusline]\nleft = []\nright = []\n", check: func(t *testing.T, a *app.App) {
			if a.Statusline.Has(ui.StatusMode) || a.Statusline.Has(ui.StatusLineNumbers) {
				t.Fatal("empty arrays restored defaults")
			}
		}},
		{name: "empty separator", config: "[statusline]\nseparator = ''\nright = []\n", check: func(t *testing.T, a *app.App) {
			if got := a.Statusline.Format(ui.StatusState{Mode: "VIEW", Filename: "file"}, 20, false).Text(); !strings.HasPrefix(got, " VIEW  file ") {
				t.Fatalf("empty separator = %q", got)
			}
		}},
		{name: "environment overrides config", config: "[statusline.styles.mode]\nbg = '#112233'\n[search.highlight-style]\nbg = '#010203'", env: map[string]string{"GLIMPSE_STATUSLINE_STYLES_MODE_BG": "#aabbcc", "GLIMPSE_SEARCH_HIGHLIGHT_STYLE_BG": "#445566"}, check: func(t *testing.T, a *app.App) {
			bar := a.Statusline.Format(ui.StatusState{Mode: "VIEW"}, 40, false)
			if bar.Spans[0].Style.GetBackground() != color.NewHexColor(0xaabbcc) || a.SearchHighlight.GetBackground() != color.NewHexColor(0x445566) {
				t.Fatal("environment colors did not override TOML")
			}
		}},
		{name: "environment-only styles", env: map[string]string{"GLIMPSE_STATUSLINE_STYLES_FILENAME_FG": "#123456", "GLIMPSE_SEARCH_HIGHLIGHT_STYLE_FG": "#abcdef"}, check: func(t *testing.T, a *app.App) {
			if a.SearchHighlight.GetForeground() != color.NewHexColor(0xabcdef) {
				t.Fatal("environment-only highlight ignored")
			}
			for _, span := range a.Statusline.Format(ui.StatusState{Filename: "file"}, 40, false).Spans {
				if span.Section == ui.StatusFilename && span.Style.GetForeground() != color.NewHexColor(0x123456) {
					t.Fatal("environment-only section color ignored")
				}
			}
		}},
		{name: "environment section lists", env: map[string]string{"GLIMPSE_STATUSLINE_LEFT": "MODE", "GLIMPSE_STATUSLINE_RIGHT": "LINE_NUMBERS"}, check: func(t *testing.T, a *app.App) {
			if a.Statusline.Has(ui.StatusFilename) || a.Statusline.Has(ui.StatusSearchResults) {
				t.Fatal("environment lists ignored")
			}
		}},
		{name: "unknown section", config: "[statusline]\nleft = ['typo']", wantErr: "unknown statusline section"},
		{name: "duplicate section", config: "[statusline]\nleft = ['mode']\nright = ['MODE']", wantErr: "duplicate statusline section"},
		{name: "scalar section list", config: "[statusline]\nleft = 'mode'", wantErr: "must be an array"},
		{name: "non-string section", config: "[statusline]\nleft = [1]", wantErr: "only section names"},
		{name: "invalid bar color", config: "[statusline]\nbg = 'navy'", wantErr: "#RRGGBB"},
		{name: "invalid section color", config: "[statusline.styles.mode]\nbg = '#123'", wantErr: "#RRGGBB"},
		{name: "invalid highlight color", config: "[search.highlight-style]\nfg = '#ghijkl'", wantErr: "#RRGGBB"},
		{name: "numeric color", config: "[statusline]\nfg = 123", wantErr: "must be a string"},
		{name: "unknown style", config: "[statusline.styles.typo]\nfg = '#ffffff'", wantErr: "unknown statusline section"},
		{name: "unknown color property", config: "[search.highlight-style]\nforeground = '#ffffff'", wantErr: "unknown color option"},
		{name: "non-table styles", config: "[statusline]\nstyles = 1", wantErr: "must be a table"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", dir)
			for _, key := range []string{"GLIMPSE_STATUSLINE_LEFT", "GLIMPSE_STATUSLINE_RIGHT", "GLIMPSE_STATUSLINE_STYLES_MODE_BG", "GLIMPSE_STATUSLINE_STYLES_FILENAME_FG", "GLIMPSE_SEARCH_HIGHLIGHT_STYLE_BG", "GLIMPSE_SEARCH_HIGHLIGHT_STYLE_FG"} {
				t.Setenv(key, "")
			}
			for key, value := range tt.env {
				t.Setenv(key, value)
			}
			path := filepath.Join(dir, "appearance.toml")
			if err := os.WriteFile(path, []byte(tt.config), 0o600); err != nil {
				t.Fatal(err)
			}
			called := false
			root := newRoot(func(_ context.Context, a *app.App, input io.ReadCloser, _ string) error {
				defer input.Close()
				defer a.Document.Close()
				called = true
				if tt.check != nil {
					tt.check(t, a)
				}
				return nil
			}, nil)
			root.SetIn(strings.NewReader(""))
			root.SetOut(io.Discard)
			root.SetErr(io.Discard)
			root.SetArgs([]string{"--config", path})
			err := root.Execute()
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want %s", err, tt.wantErr)
				}
				if called {
					t.Fatal("invalid configuration reached viewer")
				}
			} else if err != nil || !called {
				t.Fatalf("viewer called %v, error %v", called, err)
			}
		})
	}
}
