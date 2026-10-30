//go:build darwin || dragonfly || freebsd || netbsd || openbsd

package command

import "golang.org/x/sys/unix"

const ioctlGetTermios = unix.TIOCGETA
