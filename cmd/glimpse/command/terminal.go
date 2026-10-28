package command

import (
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"

	"golang.org/x/term"
)

// errStdoutNotTerminal means output is redirected; like cat, the input is then
// copied through instead of paged.
var errStdoutNotTerminal = errors.New("stdout is not a terminal")

type terminalHandle interface {
	Fd() uintptr
	Close() error
}

// requireInteractiveTerminal checks before opening input or initializing tcell.
// Stdin is deliberately not checked: a pipe is normal pager input, while keys
// come from the controlling terminal. Probing does not change terminal modes.
func requireInteractiveTerminal(output io.Writer) error {
	return checkInteractiveTerminal(output, term.IsTerminal, openControllingTerminal)
}

func checkInteractiveTerminal(output io.Writer, isTerminal func(int) bool, openTTY func() (terminalHandle, error)) error {
	fd, ok := output.(interface{ Fd() uintptr })
	if !ok || !isTerminal(int(fd.Fd())) {
		return errStdoutNotTerminal
	}
	tty, err := openTTY()
	if err != nil {
		return fmt.Errorf("glimpse requires a controlling terminal for keyboard input: %w", err)
	}
	defer tty.Close()
	if !isTerminal(int(tty.Fd())) {
		return fmt.Errorf("glimpse requires a controlling terminal for keyboard input: device is not a terminal")
	}
	return nil
}

func openControllingTerminal() (terminalHandle, error) {
	device := "/dev/tty"
	if runtime.GOOS == "windows" {
		device = "CONIN$"
	}
	return os.OpenFile(device, os.O_RDWR, 0)
}
