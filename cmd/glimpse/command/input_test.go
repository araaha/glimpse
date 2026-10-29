package command

import (
	"os"
	"testing"
	"time"
)

// A quiet writer must not keep glimpse from quitting: closing piped input
// interrupts a pending read.
func TestPipedInputCloseInterruptsRead(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	input, name, err := openInput(reader, nil)
	if err != nil || name != "" {
		t.Fatalf("openInput = %q, %v", name, err)
	}
	writer.Write([]byte("first\n"))
	buffer := make([]byte, 64)
	if n, err := input.Read(buffer); err != nil || string(buffer[:n]) != "first\n" {
		t.Fatalf("Read = %q, %v", buffer[:n], err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := input.Read(buffer)
		done <- err
	}()
	time.Sleep(20 * time.Millisecond) // Let the read wait for input.
	input.Close()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("read succeeded after close")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("close did not interrupt the pending read")
	}
}
