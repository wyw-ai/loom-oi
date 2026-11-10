//go:build windows

package main

import (
	"fmt"
	"os"

	"github.com/wyw-ai/loom-oi/apps/powershell/internal/terminal"
)

func main() {
	if err := terminal.Serve(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "PowerShell worker:", err)
		os.Exit(1)
	}
}
