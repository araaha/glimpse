package command

import (
	"io"
	"os"
	"syscall"
)

// pollableInput puts a pipe or socket on stdin into non-blocking mode, so Go's
// poller waits for its input and closing it interrupts a pending read. A
// blocking read cannot be interrupted: on quit, glimpse would wait for a quiet
// writer such as dmesg --follow to write again. Other files are returned as
// they are; os.Open already makes named pipes pollable.
func pollableInput(file *os.File) io.ReadCloser {
	info, err := file.Stat()
	if err != nil || info.Mode()&(os.ModeNamedPipe|os.ModeSocket) == 0 {
		return file
	}
	conn, err := file.SyscallConn()
	if err != nil {
		return file
	}
	// Duplicate the descriptor so that each file owns and closes its own:
	// sharing one would let a later close of the original hit whatever file
	// reused the number. Like every descriptor Go opens, the duplicate is
	// close-on-exec, set before a concurrent fork can see it, so that [[run]]
	// commands do not inherit the input and hold its pipe open.
	fd := -1
	if conn.Control(func(descriptor uintptr) {
		syscall.ForkLock.RLock()
		defer syscall.ForkLock.RUnlock()
		if fd, err = syscall.Dup(int(descriptor)); err == nil {
			syscall.CloseOnExec(fd)
		}
	}) != nil || err != nil {
		return file
	}
	if err := syscall.SetNonblock(fd, true); err != nil {
		syscall.Close(fd)
		return file
	}
	// NewFile registers a non-blocking descriptor with the poller.
	return os.NewFile(uintptr(fd), file.Name())
}
