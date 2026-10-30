//go:build unix

package app

import (
	"os/exec"
	"syscall"
)

// detach starts cmd in a separate process group, which keeps terminal signals
// for glimpse alone.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
