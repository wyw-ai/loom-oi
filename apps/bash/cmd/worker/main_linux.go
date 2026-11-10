package main

import (
	"fmt"
	"os"

	"github.com/wyw-ai/loom-oi/apps/bash/internal/terminal"
)

func main() {
	if err := terminal.Serve(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "bash worker:", err)
		os.Exit(1)
	}
}
