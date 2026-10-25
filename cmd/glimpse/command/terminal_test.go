package command

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"glimpse/internal/app"
)

type terminalOutput struct{ fd uintptr }

func (o terminalOutput) Fd() uintptr                 { return o.fd }
func (o terminalOutput) Write(p []byte) (int, error) { return len(p), nil }

type fakeTerminal struct {
	fd     uintptr
	closed bool
}

func (t *fakeTerminal) Fd() uintptr { return t.fd }
func (t *fakeTerminal) Close() error {
	t.closed = true
	return nil
}

func TestInteractiveTerminalProbe(t *testing.T) {
	openError := errors.New("no controlling terminal")
	for _, tt := range []struct {
		name      string
		stdoutTTY bool
		inputTTY  bool
		openErr   error
		wantErr   string
	}{
		{name: "interactive", stdoutTTY: true, inputTTY: true},
		{name: "redirected output", wantErr: "stdout is not a terminal"},
		{name: "no controlling terminal", stdoutTTY: true, openErr: openError, wantErr: "controlling terminal"},
		{name: "invalid controlling terminal", stdoutTTY: true, wantErr: "device is not a terminal"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tty := &fakeTerminal{fd: 11}
			opened := false
			err := checkInteractiveTerminal(terminalOutput{fd: 10}, func(fd int) bool {
				if fd == 10 {
					return tt.stdoutTTY
				}
				return fd == 11 && tt.inputTTY
			}, func() (terminalHandle, error) {
				opened = true
				if tt.openErr != nil {
					return nil, tt.openErr
				}
				return tty, nil
			})
			if tt.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want %q", err, tt.wantErr)
			}
			if opened != tt.stdoutTTY || tty.closed != (opened && tt.openErr == nil) {
				t.Fatalf("probe lifecycle: opened = %v, closed = %v", opened, tty.closed)
			}
			if tt.openErr != nil && !errors.Is(err, tt.openErr) {
				t.Fatalf("probe lost the underlying error: %v", err)
			}
		})
	}
}

func TestInteractiveTerminalRejectsFilesAndPipes(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "output")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	for _, output := range []io.Writer{file, writer, io.Discard, &bytes.Buffer{}} {
		if err := requireInteractiveTerminal(output); err == nil || !strings.Contains(err.Error(), "stdout is not a terminal") {
			t.Fatalf("output %T: error = %v", output, err)
		}
	}
}

type unreadInput struct{ read bool }

func (r *unreadInput) Read([]byte) (int, error) {
	r.read = true
	return 0, errors.New("input must not be read")
}

func TestNonInteractiveCommandRejectsBeforeOpeningInput(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, args := range [][]string{{}, {filepath.Join(t.TempDir(), "missing")}} {
		input := &unreadInput{}
		root := NewRoot()
		root.SetIn(input)
		root.SetOut(io.Discard)
		root.SetErr(io.Discard)
		root.SetArgs(args)
		if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "requires an interactive terminal") {
			t.Fatalf("args %v: error = %v", args, err)
		}
		if input.read {
			t.Fatal("noninteractive command consumed input")
		}
	}
}

func TestInteractiveCommandAllowsPipedInput(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("GLIMPSE_CMDHEIGHT", "")
	called := false
	tty := &fakeTerminal{fd: 11}
	root := newRoot(func(_ context.Context, _ *app.App, input io.ReadCloser, name string) error {
		defer input.Close()
		called = true
		data, err := io.ReadAll(input)
		if err != nil || string(data) != "piped input" || name != "stdin" {
			t.Fatalf("input = (%q, %q, %v)", data, name, err)
		}
		return nil
	}, func(output io.Writer) error {
		return checkInteractiveTerminal(output, func(fd int) bool { return fd == 10 || fd == 11 }, func() (terminalHandle, error) {
			return tty, nil
		})
	})
	root.SetIn(strings.NewReader("piped input"))
	root.SetOut(terminalOutput{fd: 10})
	root.SetArgs([]string{})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !called || !tty.closed {
		t.Fatalf("interactive pipeline: runner called = %v, probe closed = %v", called, tty.closed)
	}
}

func TestHelpWorksWithoutInteractiveTerminal(t *testing.T) {
	input := &unreadInput{}
	var output bytes.Buffer
	root := NewRoot()
	root.SetIn(input)
	root.SetOut(&output)
	root.SetErr(io.Discard)
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Usage:") || input.read {
		t.Fatalf("headless help failed: output = %q, input read = %v", output.String(), input.read)
	}
}
