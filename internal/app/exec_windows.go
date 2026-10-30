package app

import (
	"os/exec"
	"syscall"
)

// detach starts cmd in a new process group, so a Ctrl+C meant for glimpse is
// not delivered to it.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}
