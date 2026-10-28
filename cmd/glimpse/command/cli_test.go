package command

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"glimpse/internal/app"
)

func execute(t *testing.T, config string, args ...string) (string, error) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("GLIMPSE_CMDHEIGHT", "")
	if config != "" {
		if err := os.MkdirAll(filepath.Join(dir, "glimpse"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "glimpse", "glimpse.toml"), []byte(config), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	root := newRoot(func(context.Context, *app.App, io.ReadCloser, string) error {
		t.Fatal("the viewer must not start")
		return nil
	}, func(io.Writer) error { t.Fatal("no terminal check expected"); return nil })
	root.SetIn(strings.NewReader(""))
	root.SetOut(&out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func TestVersion(t *testing.T) {
	out, err := execute(t, "", "--version")
	if err != nil || !strings.HasPrefix(out, "glimpse ") {
		t.Fatalf("version = %q, %v", out, err)
	}
	Version = "v9.9.9"
	defer func() { Version = "" }()
	if version() != "v9.9.9" {
		t.Fatal("build-time version ignored")
	}
}

func TestCompletion(t *testing.T) {
	for shell, want := range map[string]string{"bash": "__start_glimpse", "zsh": "#compdef glimpse", "fish": "complete -c glimpse", "powershell": "Register-ArgumentCompleter"} {
		out, err := execute(t, "", "--completion", shell)
		if err != nil || !strings.Contains(out, want) {
			t.Fatalf("%s completion lacks %q: %v", shell, want, err)
		}
	}
	// Completion scripts call back into the hidden __complete command.
	out, err := execute(t, "", "__complete", "--follow-n")
	if err != nil || !strings.Contains(out, "--follow-name") {
		t.Fatalf("__complete = %q, %v", out, err)
	}
	if _, err := execute(t, "", "--completion", "tcsh"); err == nil || !strings.Contains(err.Error(), "unknown completion shell") {
		t.Fatalf("tcsh error = %v", err)
	}
}

func TestCheckConfig(t *testing.T) {
	out, err := execute(t, "", "--check-config")
	if err != nil || !strings.Contains(out, "no config file found; defaults: OK") {
		t.Fatalf("defaults = %q, %v", out, err)
	}
	valid := "cmdheight = -2\njump-target = 0.5\n[display]\nwrap = false\n[display.unprintable]\nchar = '?'\n[keybinds]\nhelp = ['?']\nsearch-backward = []\n[statusline.styles]\nmode = { bg = 'red' }\n"
	out, err = execute(t, valid, "--check-config")
	if err != nil || !strings.HasSuffix(out, "glimpse.toml: OK\n") {
		t.Fatalf("valid = %q, %v", out, err)
	}
	for config, want := range map[string]string{
		"cmdheigth = 1\n[search]\nsmart_case = true\n": "unknown option(s): cmdheigth, search.smart_case",
		"cmdheight = 5\n":                     "cmdheight must be",
		"[keybinds]\ntop = ['gg']\n":          "invalid key",
		"[display.unprintable]\nchar = '界'\n": "one cell wide",
	} {
		if _, err := execute(t, config, "--check-config"); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%q: error = %v, want %q", config, err, want)
		}
	}
}

func TestLabel(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		args          []string
		manPage, want string
	}{
		{[]string{"--label", "build log"}, "", "build log"},
		{nil, "ls(1)", "ls(1)"},
		{[]string{"--label", "mine"}, "ls(1)", "mine"},
		{[]string{file}, "ls(1)", ""}, // MAN_PN names stdin only
		{nil, "", ""},
	} {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		t.Setenv("MAN_PN", tt.manPage)
		root := newRoot(func(_ context.Context, application *app.App, input io.ReadCloser, _ string) error {
			defer input.Close()
			if application.Label != tt.want {
				t.Fatalf("%v with MAN_PN %q: label %q, want %q", tt.args, tt.manPage, application.Label, tt.want)
			}
			return nil
		}, nil)
		root.SetIn(strings.NewReader("text"))
		root.SetArgs(tt.args)
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
	}
}
