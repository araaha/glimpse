package app

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"glimpse/internal/keybind"

	"github.com/gdamore/tcell/v3"
)

// RunCommand binds a key sequence to a shell command, as configured by [[run]].
type RunCommand struct {
	Keys       []string
	Command    string
	Background bool // run detached instead of handing the terminal over
	label      string
}

// editCommand opens the file in the user's editor at $line, as less's v does.
const editCommand = `${VISUAL:-${EDITOR:-vi}} +"$line" "$file"`

// runBindings turns run commands into view bindings named run-1, run-2, ...
func runBindings(runs []RunCommand) ([]keybind.Binding, map[keybind.Action]RunCommand, error) {
	var bindings []keybind.Binding
	actions := make(map[keybind.Action]RunCommand, len(runs))
	for i, run := range runs {
		action := keybind.Action("run-" + strconv.Itoa(i+1))
		if len(run.Keys) == 0 {
			return nil, nil, fmt.Errorf("%s: keys must not be empty", action)
		}
		if strings.TrimSpace(run.Command) == "" {
			return nil, nil, fmt.Errorf("%s: command must not be empty", action)
		}
		keys := make([]keybind.Key, 0, len(run.Keys))
		for _, token := range run.Keys {
			key, err := keybind.ParseKey(token)
			if err != nil {
				return nil, nil, fmt.Errorf("%s: %w", action, err)
			}
			keys = append(keys, key)
		}
		bindings = append(bindings, keybind.Binding{Action: action, Keys: keys})
		run.label = keybind.Format(keys)
		actions[action] = run
	}
	return bindings, actions, nil
}

// shellCommand runs command with sh -c. line and file reach it only as
// environment variables, so their values are never parsed as shell syntax.
func shellCommand(command string, line int, file string) *exec.Cmd {
	cmd := exec.Command("sh", "-c", command)
	cmd.Env = append(os.Environ(), "line="+strconv.Itoa(line), "file="+file)
	return cmd
}

// commandFile is the absolute path for $file, or "" for stdin.
func commandFile(name string) string {
	if name == "" {
		return ""
	}
	if abs, err := filepath.Abs(name); err == nil {
		return abs
	}
	return name
}

// runForeground hands the terminal to cmd until it exits. Its input is the
// terminal, not glimpse's input, which may be a pipe.
func runForeground(screen tcell.Screen, cmd *exec.Cmd) error {
	tty, err := os.Open("/dev/tty")
	if err != nil {
		tty, err = os.Open(os.DevNull)
		if err != nil {
			return err
		}
	}
	defer tty.Close()
	cmd.Stdin, cmd.Stdout, cmd.Stderr = tty, os.Stdout, os.Stderr
	if err := screen.Suspend(); err != nil {
		return fmt.Errorf("suspend terminal: %w", err)
	}
	runErr := cmd.Run()
	if err := screen.Resume(); err != nil {
		return errors.Join(runErr, fmt.Errorf("resume terminal: %w", err))
	}
	screen.Sync()
	return runErr
}

// runBackground starts cmd without the terminal and reports a failure on
// failures; glimpse does not wait for it. Reports that find failures full are
// dropped rather than blocking.
func runBackground(cmd *exec.Cmd, label string, failures chan<- string) {
	// A separate process group keeps terminal signals for glimpse alone.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	report := func(err error) {
		select {
		case failures <- commandFailure(label, err):
		default:
		}
	}
	if err := cmd.Start(); err != nil {
		report(err)
		return
	}
	go func() {
		if err := cmd.Wait(); err != nil {
			report(err)
		}
	}()
}

func commandFailure(label string, err error) string {
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() >= 0 {
		return fmt.Sprintf("%s: exited with status %d", label, exit.ExitCode())
	}
	return fmt.Sprintf("%s: %v", label, err)
}
