//go:build linux || darwin || dragonfly || freebsd || netbsd || openbsd

package command

import (
	"errors"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// readable polls without reading, so no input is consumed. Errors count as
// ready: the reader then reports them.
func readable(file *os.File, timeout time.Duration) bool {
	conn, err := file.SyscallConn()
	if err != nil {
		return true
	}
	ready := true
	if conn.Control(func(fd uintptr) {
		fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		n, err := unix.Poll(fds, int(timeout.Milliseconds()))
		// An interrupted poll (Go preempts with signals) has seen nothing yet.
		ready = n > 0 || err != nil && !errors.Is(err, unix.EINTR)
	}) != nil {
		return true
	}
	return ready
}

func echoDisabled(tty *os.File) bool {
	termios, err := unix.IoctlGetTermios(int(tty.Fd()), ioctlGetTermios)
	return err == nil && termios.Lflag&unix.ECHO == 0
}
