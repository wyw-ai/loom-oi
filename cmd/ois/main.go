// Command ois runs the OI server (deployment A).
package main

import (
	"context"
	"os"

	"github.com/wyw-ai/loom-oi/internal/client"
	"github.com/wyw-ai/loom-oi/internal/ois/server"
)

func main() {
	ctx, args := context.Background(), os.Args[1:]
	if len(args) > 0 && args[0] == "admin" {
		// Admin calls speak the client API; the server package must not
		// import the client, whose tests run OIS through tests/topo.
		os.Exit(client.OISAdmin(ctx, args[1:], os.Stdout, os.Stderr))
	}
	os.Exit(server.Main(ctx, args, os.Stdout, os.Stderr))
}
