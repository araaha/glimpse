package command

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"glimpse/internal/app"
	"glimpse/internal/keybind"
)

func TestCmdHeightConfiguration(t *testing.T) {
	const noTerminal = "runner called"
	tests := []struct {
		name    string
		config  string
		env     string
		args    []string
		wantErr string
	}{
		{name: "default", wantErr: noTerminal},
		{name: "toggle flag", args: []string{"--cmdheight=-1"}, wantErr: noTerminal},
		{name: "status flag", args: []string{"--cmdheight=0"}, wantErr: noTerminal},
		{name: "search flag", args: []string{"--cmdheight=1"}, wantErr: noTerminal},
		{name: "invalid negative flag", args: []string{"--cmdheight=-2"}, wantErr: "cmdheight must be -1, 0, or 1"},
		{name: "invalid positive flag", args: []string{"--cmdheight=2"}, wantErr: "cmdheight must be -1, 0, or 1"},
		{name: "TOML", config: "cmdheight = 1\n", wantErr: noTerminal},
		{name: "invalid TOML value", config: "cmdheight = 2\n", wantErr: "cmdheight must be -1, 0, or 1"},
		{name: "environment", env: "-1", wantErr: noTerminal},
		{name: "invalid environment", env: "2", wantErr: "cmdheight must be -1, 0, or 1"},
		{name: "noninteger environment", env: "invalid", wantErr: "parse cmdheight"},
		{name: "noninteger TOML value", config: "cmdheight = 'invalid'\n", wantErr: "parse cmdheight"},
		{name: "flag overrides TOML", config: "cmdheight = 2\n", args: []string{"--cmdheight=0"}, wantErr: noTerminal},
		{name: "environment overrides TOML", config: "cmdheight = 2\n", env: "0", wantErr: noTerminal},
		{name: "flag overrides environment", env: "2", args: []string{"--cmdheight=0"}, wantErr: noTerminal},
		{name: "key sequence", config: "[keybinds]\ntop = ['g', 'g']\n", wantErr: noTerminal},
		{name: "not an array", config: "[keybinds]\ntop = 'gg'\n", wantErr: "must be an array of keys"},
		{name: "non-string key", config: "[keybinds]\ntop = [1]\n", wantErr: "non-string key"},
		{name: "unknown action", config: "[keybinds]\nunknown = ['x']\n", wantErr: "unknown keybind action"},
		{name: "invalid token", config: "[keybinds]\ntop = ['gg']\n", wantErr: "invalid key"},
		{name: "override default binding", config: "[keybinds]\ntop = ['G']\n", wantErr: noTerminal},
		{name: "rebind Ctrl-C and Escape", config: "[keybinds]\nup = ['<C-c>']\ntop = ['<Esc>']\n", wantErr: noTerminal},
		{name: "binding conflict", config: "[keybinds]\ntop = ['x']\nbottom = ['x']\n", wantErr: "bound to both"},
		{name: "invalid chunk size", args: []string{"--chunk-size=0"}, wantErr: "chunk-size must be a positive integer"},
		{name: "search bindings", config: "[keybinds]\nsearch-word-left = ['<M-h>']\nsearch-accept = ['<C-s>']\n", wantErr: noTerminal},
		{name: "invalid search action", config: "[keybinds]\nsearch-unknown = ['q']\n", wantErr: "unknown keybind action"},
		{name: "invalid smart case", config: "[search]\nsmart-case = 'bad'\n", wantErr: "parse search.smart-case"},
		{name: "invalid ignore case", config: "[search]\nignore-case = 'bad'\n", wantErr: "parse search.ignore-case"},
		{name: "invalid case sensitive", config: "[search]\ncase-sensitive = 'bad'\n", wantErr: "parse search.case-sensitive"},
		{name: "too many files", args: []string{"one", "two"}, wantErr: "at most 1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configDir := t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", configDir)
			t.Setenv("GLIMPSE_CMDHEIGHT", tt.env)
			if tt.config != "" {
				dir := filepath.Join(configDir, "glimpse")
				if err := os.Mkdir(dir, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "glimpse.toml"), []byte(tt.config), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			root := newRoot(func(_ context.Context, _ *app.App, input io.ReadCloser, _ string) error {
				input.Close()
				return errors.New(noTerminal)
			}, nil)
			root.SetIn(strings.NewReader(""))
			root.SetOut(io.Discard)
			root.SetErr(io.Discard)
			root.SetArgs(append([]string{}, tt.args...))
			if err := root.Execute(); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Execute() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestSearchCaseConfiguration(t *testing.T) {
	for _, tt := range []struct {
		name, config, env, ignoreEnv string
		args                         []string
		want, wantIgnore             bool
	}{
		{name: "default"},
		{name: "flag", args: []string{"--smart-case"}, want: true},
		{name: "TOML", config: "[search]\nsmart-case = true\n", want: true},
		{name: "environment", env: "true", want: true},
		{name: "environment overrides TOML", config: "[search]\nsmart-case = true\n", env: "false"},
		{name: "flag overrides environment", env: "true", args: []string{"--smart-case=false"}},
		{name: "ignore-case flag", args: []string{"--ignore-case"}, wantIgnore: true},
		{name: "ignore-case TOML", config: "[search]\nignore-case = true\n", wantIgnore: true},
		{name: "ignore-case environment", ignoreEnv: "true", wantIgnore: true},
		{name: "case-sensitive flag overrides smart-case TOML", config: "[search]\nsmart-case = true\n", args: []string{"--case-sensitive"}},
		{name: "ignore-case flag overrides smart-case TOML", config: "[search]\nsmart-case = true\n", args: []string{"--ignore-case"}, wantIgnore: true},
		{name: "smart-case flag overrides case-sensitive TOML", config: "[search]\ncase-sensitive = true\n", args: []string{"--smart-case"}, want: true},
		{name: "ignore-case env overrides smart-case TOML", config: "[search]\nsmart-case = true\n", ignoreEnv: "true", wantIgnore: true},
		{name: "smart-case wins within tier", config: "[search]\nsmart-case = true\nignore-case = true\n", want: true},
		{name: "case-sensitive wins within tier", config: "[search]\nsmart-case = true\nignore-case = true\ncase-sensitive = true\n"},
		{name: "explicit false reveals configured fallback", config: "[search]\nsmart-case = true\nignore-case = true\n", args: []string{"--smart-case=false"}, wantIgnore: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", dir)
			t.Setenv("GLIMPSE_SEARCH_SMART_CASE", tt.env)
			t.Setenv("GLIMPSE_SEARCH_IGNORE_CASE", tt.ignoreEnv)
			t.Setenv("GLIMPSE_SEARCH_CASE_SENSITIVE", "")
			path := filepath.Join(dir, "test.toml")
			if err := os.WriteFile(path, []byte(tt.config), 0o600); err != nil {
				t.Fatal(err)
			}
			called := false
			root := newRoot(func(_ context.Context, application *app.App, input io.ReadCloser, _ string) error {
				defer input.Close()
				called = true
				if application.SmartCase != tt.want || application.IgnoreCase != tt.wantIgnore {
					t.Fatalf("SmartCase, IgnoreCase = %v, %v; want %v, %v", application.SmartCase, application.IgnoreCase, tt.want, tt.wantIgnore)
				}
				return nil
			}, nil)
			root.SetIn(strings.NewReader(""))
			root.SetArgs(append([]string{"--config", path}, tt.args...))
			if err := root.Execute(); err != nil {
				t.Fatal(err)
			}
			if !called {
				t.Fatal("viewer not called")
			}
		})
	}
}

func TestRootLoadsFileAndKeySequence(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("GLIMPSE_CMDHEIGHT", "")
	configPath := filepath.Join(dir, "test.toml")
	if err := os.WriteFile(configPath, []byte("cmdheight = 1\n[keybinds]\ntop = ['t', 't']\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	inputPath := filepath.Join(dir, "input.txt")
	if err := os.WriteFile(inputPath, []byte("file input"), 0o600); err != nil {
		t.Fatal(err)
	}
	called := false
	root := newRoot(func(_ context.Context, application *app.App, input io.ReadCloser, name string) error {
		defer input.Close()
		called = true
		data, err := io.ReadAll(input)
		if err != nil || string(data) != "file input" || name != inputPath {
			t.Fatalf("input = (%q, %v), name = %q", data, err, name)
		}
		if layout := application.UI.Layout(80, 24); layout.StatusRow != 22 || layout.SearchRow != 23 {
			t.Fatalf("configured layout = %+v", layout)
		}
		if got := application.Keys.Feed("t"); len(got) != 0 {
			t.Fatal("first key fired the action")
		}
		if got := application.Keys.Feed("t"); len(got) != 1 || got[0].Action != keybind.Top {
			t.Fatalf("configured sequence = %v", got)
		}
		return nil
	}, nil)
	root.SetArgs([]string{"--config", configPath, inputPath})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("command did not start the viewer")
	}
}

func TestOpenInput(t *testing.T) {
	for _, args := range [][]string{nil, {"-"}} {
		input, name, err := openInput(strings.NewReader("pipe data"), args)
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(input)
		input.Close()
		if err != nil || name != "stdin" || string(data) != "pipe data" {
			t.Fatalf("stdin input = (%q, %q, %v)", data, name, err)
		}
	}
	dir := t.TempDir()
	if input, _, err := openInput(strings.NewReader(""), []string{dir}); err == nil {
		input.Close()
		t.Fatal("accepted a directory as input")
	}
	if input, _, err := openInput(strings.NewReader(""), []string{filepath.Join(dir, "missing")}); err == nil {
		input.Close()
		t.Fatal("accepted a missing file")
	}
}
