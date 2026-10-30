//go:build !(linux || darwin || dragonfly || freebsd || netbsd || openbsd)

package command

import (
	"os"
	"time"
)

func readable(*os.File, time.Duration) bool { return true }

func echoDisabled(*os.File) bool { return false }
