// Command oi is the OI consumer and administration client.
package main

import (
	"context"
	"os"

	"github.com/wyw-ai/loom-oi/internal/client"
)

func main() {
	os.Exit(client.Main(context.Background(), os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
