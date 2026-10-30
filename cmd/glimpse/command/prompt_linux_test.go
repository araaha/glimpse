package command

import (
	"os"
	"strconv"
	"testing"

	"golang.org/x/sys/unix"
)

func TestEchoDisabled(t *testing.T) {
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Skip("no pseudo-terminals:", err)
	}
	defer master.Close()
	if err := unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		t.Fatal(err)
	}
	n, err := unix.IoctlGetUint32(int(master.Fd()), unix.TIOCGPTN)
	if err != nil {
		t.Fatal(err)
	}
	tty, err := os.OpenFile("/dev/pts/"+strconv.Itoa(int(n)), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer tty.Close()
	termios, err := unix.IoctlGetTermios(int(tty.Fd()), unix.TCGETS)
	if err != nil {
		t.Fatal(err)
	}
	termios.Lflag |= unix.ECHO
	unix.IoctlSetTermios(int(tty.Fd()), unix.TCSETS, termios)
	if echoDisabled(tty) {
		t.Fatal("echo reported off")
	}
	// What sudo does while reading a password.
	termios.Lflag &^= unix.ECHO
	unix.IoctlSetTermios(int(tty.Fd()), unix.TCSETS, termios)
	if !echoDisabled(tty) {
		t.Fatal("echo reported on")
	}
}
