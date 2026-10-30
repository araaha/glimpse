package command

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestAwaitPrompt(t *testing.T) {
	const grace = 100 * time.Millisecond
	for _, tt := range []struct {
		name          string
		readyAfter    time.Duration // input arrives this long after the start
		echoOffUntil  time.Duration // another process reads a password until then
		atLeast, upTo time.Duration
	}{
		{name: "input at once", readyAfter: 0, upTo: grace / 2},
		{name: "silent input waits only the grace", readyAfter: time.Hour, atLeast: grace, upTo: 3 * grace},
		{name: "input during the grace", readyAfter: grace / 2, atLeast: grace / 2, upTo: grace},
		// Echo is sampled every 50ms, so the grace may start up to a step early.
		{name: "a password prompt holds the terminal", readyAfter: time.Hour, echoOffUntil: 4 * grace, atLeast: 5*grace - 50*time.Millisecond, upTo: 7 * grace},
		{name: "input after the password", readyAfter: 4*grace + grace/2, echoOffUntil: 4 * grace, atLeast: 4 * grace, upTo: 5 * grace},
	} {
		t.Run(tt.name, func(t *testing.T) {
			start := time.Now()
			ready := func(timeout time.Duration) bool {
				if time.Since(start) >= tt.readyAfter {
					return true
				}
				time.Sleep(min(timeout, tt.readyAfter-time.Since(start)))
				return time.Since(start) >= tt.readyAfter
			}
			echoOff := func() bool { return time.Since(start) < tt.echoOffUntil }
			awaitPrompt(context.Background(), ready, echoOff, grace)
			if waited := time.Since(start); waited < tt.atLeast || waited > tt.upTo {
				t.Fatalf("waited %v, want %v to %v", waited, tt.atLeast, tt.upTo)
			}
		})
	}
}

func TestAwaitPromptStopsWhenCanceled(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	never := func(timeout time.Duration) bool { time.Sleep(timeout); return false }
	awaitPrompt(ctx, never, func() bool { return true }, time.Hour)
	if waited := time.Since(start); waited > 300*time.Millisecond {
		t.Fatalf("canceled wait took %v", waited)
	}
}

func TestReadableDoesNotConsumeInput(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if readable(reader, 10*time.Millisecond) {
		t.Fatal("empty pipe is readable")
	}
	writer.Write([]byte("x"))
	if !readable(reader, 10*time.Millisecond) || !readable(reader, 0) {
		t.Fatal("pipe with data is not readable")
	}
	buf := make([]byte, 2)
	if n, _ := reader.Read(buf); n != 1 || buf[0] != 'x' {
		t.Fatalf("poll consumed input: %q", buf[:n])
	}
	writer.Close()
	if !readable(reader, 10*time.Millisecond) {
		t.Fatal("EOF is not readable")
	}
}
