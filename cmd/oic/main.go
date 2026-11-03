// Command oic runs an OI client agent for one Access.
package main

import (
	"context"
	"os"

	"github.com/wyw-ai/loom-oi/internal/oic/relay"
)

func main() {
	os.Exit(relay.Main(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}
