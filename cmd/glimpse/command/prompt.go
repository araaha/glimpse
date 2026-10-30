package command

import (
	"context"
	"os"
	"time"
)

// promptGrace is how long silent piped input may delay taking over the
// terminal, both at start and after another process has finished reading a
// password. A command that writes at once is shown at once.
const promptGrace = 500 * time.Millisecond

// awaitPrompt waits before glimpse takes over the terminal while piped input
// is silent and another process may still be prompting on it, as in
// sudo cmd | glimpse. sudo, ssh, and gpg turn echo off while reading a
// password; until echo returns, the keys must reach them, not glimpse.
// ready reports whether input became readable (or reached EOF) within the
// given time; echoOff reports whether terminal echo is off. Cancellation
// (Ctrl-C) ends the wait.
func awaitPrompt(ctx context.Context, ready func(time.Duration) bool, echoOff func() bool, grace time.Duration) {
	const step = 50 * time.Millisecond
	deadline := time.Now().Add(grace)
	for ctx.Err() == nil && !ready(step) {
		if echoOff() {
			deadline = time.Now().Add(grace)
		} else if time.Now().After(deadline) {
			return
		}
	}
}

// awaitPipedPrompt applies awaitPrompt to a pipe or socket on stdin, with the
// controlling terminal's echo setting.
func awaitPipedPrompt(ctx context.Context, input any) {
	file, ok := input.(*os.File)
	if !ok {
		return
	}
	if info, err := file.Stat(); err != nil || info.Mode()&(os.ModeNamedPipe|os.ModeSocket) == 0 {
		return
	}
	tty, err := os.Open("/dev/tty")
	if err != nil {
		return
	}
	defer tty.Close()
	awaitPrompt(ctx, func(timeout time.Duration) bool { return readable(file, timeout) }, func() bool { return echoDisabled(tty) }, promptGrace)
}
