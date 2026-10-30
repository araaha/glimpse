//go:build linux || darwin || dragonfly || freebsd || netbsd || openbsd

package command

import (
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

func TestPollableInputIsCloseOnExec(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	input := pollableInput(reader)
	defer input.Close()
	file, ok := input.(*os.File)
	if !ok || file == reader {
		t.Fatal("pipe was not duplicated")
	}
	conn, err := file.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var flags int
	if controlErr := conn.Control(func(fd uintptr) { flags, err = unix.FcntlInt(fd, unix.F_GETFD, 0) }); controlErr != nil {
		t.Fatal(controlErr)
	}
	if err != nil || flags&unix.FD_CLOEXEC == 0 {
		t.Fatalf("duplicate descriptor flags = %#x, %v; child commands would inherit the pipe", flags, err)
	}
}
