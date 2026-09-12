// Command picklock-mcp runs picklock as a Model Context Protocol (MCP) server
// over stdio (stdin/stdout JSON-RPC), exposing database browsing and
// querying tools to AI agents.
//
// Usage:
//
//	picklock-mcp [--read-only] [DSN]
//
// If a DSN is provided the server connects immediately; otherwise the
// AI agent must call the "connect" tool first.
package main

import (
	"flag"
	"fmt"
	"os"

	_ "github.com/agmonetti/picklock/internal/store/cassandra"
	_ "github.com/agmonetti/picklock/internal/store/mongo"
	_ "github.com/agmonetti/picklock/internal/store/mssql"
	_ "github.com/agmonetti/picklock/internal/store/mysql"
	_ "github.com/agmonetti/picklock/internal/store/neo4j"
	_ "github.com/agmonetti/picklock/internal/store/postgres"
	_ "github.com/agmonetti/picklock/internal/store/redis"
	_ "github.com/agmonetti/picklock/internal/store/sqlite"

	picklockMCP "github.com/agmonetti/picklock/internal/mcp"
)

func main() {
	readOnly := flag.Bool("read-only", false, "block all mutation queries (default: allow writes)")
	flag.Parse()

	if flag.NArg() > 1 {
		fmt.Fprintln(os.Stderr, "picklock-mcp: too many arguments — usage: picklock-mcp [--read-only] [DSN]")
		os.Exit(1)
	}

	srv := picklockMCP.New(*readOnly)
	defer srv.Close()

	// If a DSN was provided, connect before starting the server.
	if flag.NArg() == 1 {
		if err := srv.ConnectDSN(flag.Arg(0)); err != nil {
			fmt.Fprintf(os.Stderr, "picklock-mcp: %v\n", err)
			os.Exit(1)
		}
	}

	if err := srv.Serve(); err != nil {
		fmt.Fprintf(os.Stderr, "picklock-mcp: %v\n", err)
		os.Exit(1)
	}
}
