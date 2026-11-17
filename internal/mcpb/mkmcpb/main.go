// Command mkmcpb writes SockDock's MCP Bundle for a release.
//
//	mkmcpb <out.mcpb> <sockdock CLI> <version>
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/iwamatsu0430/sock-dock-slack/internal/mcpb"
)

func main() {
	if len(os.Args) != 4 {
		fmt.Fprintln(os.Stderr, "usage: mkmcpb <out.mcpb> <sockdock CLI> <version>")
		os.Exit(2)
	}
	if err := mcpb.Build(context.Background(), os.Args[1], os.Args[2], os.Args[3]); err != nil {
		fmt.Fprintln(os.Stderr, "mkmcpb:", err)
		os.Exit(1)
	}
}
