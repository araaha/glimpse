//go:build linux || darwin

package command

import (
	"testing"

	"golang.org/x/sys/unix"
)

func TestEchoDisabled(t *testing.T) {
	master, tty := openPTY(t)
	defer master.Close()
	defer tty.Close()
	termios, err := unix.IoctlGetTermios(int(tty.Fd()), ioctlGetTermios)
	if err != nil {
		t.Fatal(err)
	}
	termios.Lflag |= unix.ECHO
	unix.IoctlSetTermios(int(tty.Fd()), ioctlSetTermios, termios)
	if echoDisabled(tty) {
		t.Fatal("echo reported off")
	}
	// What sudo does while reading a password.
	termios.Lflag &^= unix.ECHO
	unix.IoctlSetTermios(int(tty.Fd()), ioctlSetTermios, termios)
	if !echoDisabled(tty) {
		t.Fatal("echo reported on")
	}
}
