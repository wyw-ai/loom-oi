// Command ois runs the OI server (deployment A).
package main

import (
	"context"
	"os"

	"github.com/wyw-ai/loom-oi/internal/ois/server"
)

func main() {
	os.Exit(server.Main(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}
