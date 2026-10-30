package command

import (
	"bytes"
	"os"
	"testing"
	"unsafe"

	"golang.org/x/sys/unix"
)

const ioctlSetTermios = unix.TIOCSETA

func openPTY(t *testing.T) (master, tty *os.File) {
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Skip("no pseudo-terminals:", err)
	}
	fd := master.Fd()
	var name [128]byte
	for _, req := range []uintptr{unix.TIOCPTYGRANT, unix.TIOCPTYUNLK, unix.TIOCPTYGNAME} {
		arg := uintptr(0)
		if req == unix.TIOCPTYGNAME {
			arg = uintptr(unsafe.Pointer(&name[0]))
		}
		if _, _, errno := unix.Syscall(unix.SYS_IOCTL, fd, req, arg); errno != 0 {
			master.Close()
			t.Fatal(errno)
		}
	}
	path := string(name[:bytes.IndexByte(name[:], 0)])
	tty, err = os.OpenFile(path, os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		master.Close()
		t.Fatal(err)
	}
	return master, tty
}
