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
	var fd uintptr
	if conn.Control(func(descriptor uintptr) { fd = descriptor }) != nil || syscall.SetNonblock(int(fd), true) != nil {
		return file
	}
	// NewFile registers a non-blocking descriptor with the poller. It takes
	// over the descriptor, which the original file must no longer use.
	return os.NewFile(fd, file.Name())
}
