package command

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/araaha/glimpse/internal/app"
	"github.com/araaha/glimpse/internal/keybind"

	"github.com/spf13/viper"
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
		{name: "hidden toggle flag", args: []string{"--cmdheight=-2"}, wantErr: noTerminal},
		{name: "invalid negative flag", args: []string{"--cmdheight=-3"}, wantErr: "cmdheight must be -2, -1, 0, or 1"},
		{name: "invalid positive flag", args: []string{"--cmdheight=2"}, wantErr: "cmdheight must be -2, -1, 0, or 1"},
		{name: "TOML", config: "cmdheight = 1\n", wantErr: noTerminal},
		{name: "invalid TOML value", config: "cmdheight = 2\n", wantErr: "cmdheight must be -2, -1, 0, or 1"},
		{name: "environment", env: "-1", wantErr: noTerminal},
		{name: "invalid environment", env: "2", wantErr: "cmdheight must be -2, -1, 0, or 1"},
		{name: "noninteger environment", env: "invalid", wantErr: "'cmdheight' must be an integer"},
		{name: "noninteger TOML value", config: "cmdheight = 'invalid'\n", wantErr: "'cmdheight' must be an integer"},
		{name: "flag overrides TOML", config: "cmdheight = 2\n", args: []string{"--cmdheight=0"}, wantErr: noTerminal},
		{name: "environment overrides TOML", config: "cmdheight = 2\n", env: "0", wantErr: noTerminal},
		{name: "flag overrides environment", env: "2", args: []string{"--cmdheight=0"}, wantErr: noTerminal},
		{name: "key sequence", config: "[keybinds]\ntop = ['g', 'g']\n", wantErr: noTerminal},
		{name: "not an array", config: "[keybinds]\ntop = 'gg'\n", wantErr: "'keybinds[top]' source data must be an array"},
		{name: "non-string key", config: "[keybinds]\ntop = [1]\n", wantErr: "'keybinds[top][0]' expected type 'string'"},
		{name: "unknown action", config: "[keybinds]\nunknown = ['x']\n", wantErr: "unknown keybind action"},
		{name: "invalid token", config: "[keybinds]\ntop = ['gg']\n", wantErr: "invalid key"},
		{name: "override default binding", config: "[keybinds]\ntop = ['G']\n", wantErr: noTerminal},
		{name: "rebind Ctrl-C and Escape", config: "[keybinds]\nup = ['<C-c>']\ntop = ['<Esc>']\n", wantErr: noTerminal},
		{name: "binding conflict", config: "[keybinds]\ntop = ['x']\nbottom = ['x']\n", wantErr: "bound to both"},
		{name: "invalid chunk size", args: []string{"--chunk-size=0"}, wantErr: "chunk-size must be a positive integer"},
		{name: "search bindings", config: "[keybinds]\nsearch-word-left = ['<M-h>']\nsearch-accept = ['<C-s>']\n", wantErr: noTerminal},
		{name: "invalid search action", config: "[keybinds]\nsearch-unknown = ['q']\n", wantErr: "unknown keybind action"},
		{name: "invalid smart case", config: "[search]\nsmart-case = 'bad'\n", wantErr: "'search.smart-case' must be true or false"},
		{name: "invalid ignore case", config: "[search]\nignore-case = 'bad'\n", wantErr: "'search.ignore-case' must be true or false"},
		{name: "invalid case sensitive", config: "[search]\ncase-sensitive = 'bad'\n", wantErr: "'search.case-sensitive' must be true or false"},
		{name: "storage flags", args: []string{"--memory-budget=64", "--spool-quota=128", "--metadata-budget=16384", "--retention=rolling"}, wantErr: noTerminal},
		{name: "storage TOML", config: "[storage]\nmemory-budget = 64\nspool-quota = 128\nretention = 'rolling'\n", wantErr: noTerminal},
		{name: "invalid retention", args: []string{"--retention=drop"}, wantErr: "retention must be lossless or rolling"},
		{name: "invalid memory budget", args: []string{"--memory-budget=0"}, wantErr: "positive integer byte count"},
		{name: "invalid metadata budget", args: []string{"--metadata-budget=64"}, wantErr: "metadata-budget must be at least"},
		{name: "ambiguous width flag", args: []string{"--ambiguous-width=2"}, wantErr: noTerminal},
		{name: "ambiguous width TOML", config: "[display]\nambiguous-width = 2\n", wantErr: noTerminal},
		{name: "wrap flag", args: []string{"--wrap=false"}, wantErr: noTerminal},
		{name: "unprintable TOML", config: "[display.unprintable]\nchar = '·'\nfg = 'red'\nbg = '#202020'\n", wantErr: noTerminal},
		{name: "unprintable wide char", config: "[display.unprintable]\nchar = '界'\n", wantErr: "one cell wide"},
		{name: "unprintable ambiguous char when wide", config: "[display]\nambiguous-width = 2\n[display.unprintable]\nchar = '·'\n", wantErr: "one cell wide"},
		{name: "unprintable two chars", config: "[display.unprintable]\nchar = 'ab'\n", wantErr: "exactly one character"},
		{name: "unprintable control char", config: "[display.unprintable]\nchar = \"\\u0007\"\n", wantErr: "control"},
		{name: "unprintable bad color", config: "[display.unprintable]\nfg = 'nope'\n", wantErr: "unprintable"},
		{name: "unprintable unknown key", config: "[display.unprintable]\nbold = true\n", wantErr: "'display.unprintable' has invalid keys: bold"},
		{name: "follow-name flag", args: []string{"--follow-name"}, wantErr: noTerminal},
		{name: "follow-name TOML", config: "follow-name = true\n", wantErr: noTerminal},
		{name: "invalid follow-name", config: "follow-name = 'maybe'\n", wantErr: "'follow-name' must be true or false"},
		{name: "line numbers flag", args: []string{"-N"}, wantErr: noTerminal},
		{name: "line numbers TOML", config: "[display]\nline-numbers = true\n[display.line-number-style]\nfg = 'yellow'\n", wantErr: noTerminal},
		{name: "invalid line numbers", config: "[display]\nline-numbers = 'some'\n", wantErr: "'display.line-numbers' must be true or false"},
		{name: "invalid line number color", config: "[display.line-number-style]\nfg = 'nope'\n", wantErr: "line numbers"},
		{name: "unknown line number style key", config: "[display.line-number-style]\nbold = true\n", wantErr: "'display.line-number-style' has invalid keys: bold"},
		{name: "wrap TOML", config: "[display]\nwrap = false\n", wantErr: noTerminal},
		{name: "invalid wrap", config: "[display]\nwrap = 'sometimes'\n", wantErr: "'display.wrap' must be true or false"},
		{name: "invalid ambiguous width", args: []string{"--ambiguous-width=3"}, wantErr: "ambiguous-width must be 1 or 2"},
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

func TestSearchRegexConfiguration(t *testing.T) {
	for _, tt := range []struct {
		name, config, env, wantErr string
		args                       []string
		want                       bool
	}{
		{name: "default", want: true},
		{name: "TOML", config: "[search]\nregex = false\n"},
		{name: "environment overrides TOML", config: "[search]\nregex = false\n", env: "true", want: true},
		{name: "flag overrides environment", env: "true", args: []string{"--regex=false"}},
		{name: "invalid", config: "[search]\nregex = 'maybe'\n", wantErr: "search.regex"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", dir)
			t.Setenv("GLIMPSE_SEARCH_REGEX", tt.env)
			path := filepath.Join(dir, "test.toml")
			if err := os.WriteFile(path, []byte(tt.config), 0o600); err != nil {
				t.Fatal(err)
			}
			root := newRoot(func(_ context.Context, application *app.App, input io.ReadCloser, _ string) error {
				defer input.Close()
				if application.Magic != tt.want {
					t.Fatalf("Magic = %v, want %v", application.Magic, tt.want)
				}
				return nil
			}, nil)
			root.SetIn(strings.NewReader(""))
			root.SetArgs(append([]string{"--config", path}, tt.args...))
			err := root.Execute()
			if tt.wantErr == "" && err != nil || tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestJumpTargetConfiguration(t *testing.T) {
	for _, tt := range []struct {
		name, config, env, wantErr string
		args                       []string
		want                       string
	}{
		{name: "default", want: "1"},
		{name: "short flag", args: []string{"-j.75"}, want: ".75"},
		{name: "long flag fraction", args: []string{"--jump-target=.5"}, want: ".5"},
		{name: "zero", args: []string{"-j0"}, want: "0"},
		{name: "one", args: []string{"--jump-target=1"}, want: "1"},
		{name: "decimal one", args: []string{"-j1.0"}, want: "1"},
		{name: "TOML integer", config: "jump-target = 0\n", want: "0"},
		{name: "TOML float", config: "jump-target = 0.25\n", want: ".25"},
		{name: "TOML string", config: "jump-target = '.5'\n", want: ".5"},
		{name: "environment overrides TOML", config: "jump-target = 0\n", env: ".5", want: ".5"},
		{name: "flag overrides environment", env: ".5", args: []string{"-j.75"}, want: ".75"},
		{name: "negative flag", args: []string{"-j", "-2"}, wantErr: "between 0 and 1"},
		{name: "integer out of range", args: []string{"-j4"}, wantErr: "between 0 and 1"},
		{name: "fraction out of range", config: "jump-target = 1.5\n", wantErr: "between 0 and 1"},
		{name: "NaN", args: []string{"-jNaN"}, wantErr: "between 0 and 1"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", dir)
			t.Setenv("GLIMPSE_JUMP_TARGET", tt.env)
			path := filepath.Join(dir, "test.toml")
			if err := os.WriteFile(path, []byte(tt.config), 0o600); err != nil {
				t.Fatal(err)
			}
			root := newRoot(func(_ context.Context, application *app.App, input io.ReadCloser, _ string) error {
				defer input.Close()
				if got := application.JumpTarget.String(); got != tt.want {
					t.Fatalf("JumpTarget = %s, want %s", got, tt.want)
				}
				return nil
			}, nil)
			root.SetIn(strings.NewReader(""))
			root.SetArgs(append([]string{"--config", path}, tt.args...))
			err := root.Execute()
			if tt.wantErr == "" && err != nil || tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("error = %v, want %q", err, tt.wantErr)
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
		if err != nil || name != "" || string(data) != "pipe data" {
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

func TestEnvironmentAndTOMLValueTypes(t *testing.T) {
	for _, tt := range []struct {
		name, config string
		env          map[string]string
		wantErr      string
		check        func(app.Options) bool
	}{
		{name: "environment booleans and integers", env: map[string]string{"GLIMPSE_DISPLAY_WRAP": "false", "GLIMPSE_STORAGE_MEMORY_BUDGET": "4096", "GLIMPSE_FOLLOW": "1"},
			check: func(o app.Options) bool { return o.NoWrap && o.Storage.MemoryBudget == 4096 && o.Follow }},
		{name: "environment jump target", env: map[string]string{"GLIMPSE_JUMP_TARGET": "0"},
			check: func(o app.Options) bool { return o.JumpTarget.String() == "0" }},
		{name: "numeric TOML jump target", config: "jump-target = 0.75\n",
			check: func(o app.Options) bool { return o.JumpTarget.String() == ".75" }},
		{name: "quoted TOML boolean", config: "[display]\nwrap = 'false'\n", check: func(o app.Options) bool { return o.NoWrap }},
		{name: "unknown plain keys are ignored at startup", config: "colour = 1\n[search]\nsmartcase = true\n[storage]\nquota = 1\n",
			check: func(o app.Options) bool { return !o.SmartCase }},
		{name: "environment integer", env: map[string]string{"GLIMPSE_STORAGE_SPOOL_QUOTA": "lots"}, wantErr: "'storage.spool-quota' must be an integer"},
		{name: "fractional integer", config: "chunk-size = 1.5\n", wantErr: "'chunk-size' must be an integer, not 1.5"},
		{name: "whole float integer", config: "[storage]\nmemory-budget = 8192.0\n", check: func(o app.Options) bool { return o.Storage.MemoryBudget == 8192 }},
		{name: "numeric label", config: "label = 7\n", wantErr: "'label' expected type 'string'"},
		{name: "table instead of value", config: "[follow]\nx = 1\n", wantErr: "'follow'"},
		{name: "several errors are each reported", config: "follow = 'x'\n[display]\nwrap = 'y'\n", wantErr: "'follow' must be true or false, not \"x\"\nconfig: 'display.wrap' must be true or false"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", dir)
			t.Setenv("GLIMPSE_CMDHEIGHT", "")
			for key, value := range tt.env {
				t.Setenv(key, value)
			}
			path := filepath.Join(dir, "glimpse.toml")
			if err := os.WriteFile(path, []byte(tt.config), 0o600); err != nil {
				t.Fatal(err)
			}
			root := newRoot(nil, nil)
			if err := root.ParseFlags(nil); err != nil {
				t.Fatal(err)
			}
			config := viper.New()
			if err := loadConfig(config, root, path); err != nil {
				t.Fatal(err)
			}
			got, err := readOptions(config, root)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil || !tt.check(got) {
				t.Fatalf("options %+v, error %v", got, err)
			}
		})
	}
}

func TestConfigDir(t *testing.T) {
	home := t.TempDir()
	config := t.TempDir()
	t.Setenv("HOME", home)
	for _, tt := range []struct{ xdg, want string }{
		{config, config},
		{"", filepath.Join(home, ".config")},
		{"relative", filepath.Join(home, ".config")},
	} {
		t.Setenv("XDG_CONFIG_HOME", tt.xdg)
		got, err := configDir()
		if got != tt.want || err != nil {
			t.Fatalf("configDir() = (%q, %v), want %q", got, err, tt.want)
		}
	}
}
