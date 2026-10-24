package main

import (
	"fmt"
	"os"

	"glimpse/cmd/glimpse/command"
)

func main() {
	if err := command.NewRoot().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
