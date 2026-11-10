package main

import (
	"fmt"
	"github.com/wyw-ai/loom-oi/internal/terminal"
	"os"
)

func main() {
	if err := terminal.Serve(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "terminal worker:", err)
		os.Exit(1)
	}
}
