package command

import (
	"os"
	"strconv"
	"testing"

	"golang.org/x/sys/unix"
)

const ioctlSetTermios = unix.TCSETS

func openPTY(t *testing.T) (master, tty *os.File) {
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Skip("no pseudo-terminals:", err)
	}
	if err := unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		master.Close()
		t.Fatal(err)
	}
	n, err := unix.IoctlGetUint32(int(master.Fd()), unix.TIOCGPTN)
	if err != nil {
		master.Close()
		t.Fatal(err)
	}
	tty, err = os.OpenFile("/dev/pts/"+strconv.Itoa(int(n)), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		master.Close()
		t.Fatal(err)
	}
	return master, tty
}
