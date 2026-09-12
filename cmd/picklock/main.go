// Command picklock is a terminal database browser.
package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/agmonetti/picklock/internal/conn"
	_ "github.com/agmonetti/picklock/internal/store/cassandra" // registers Cassandra
	_ "github.com/agmonetti/picklock/internal/store/mongo"     // registers MongoDB
	_ "github.com/agmonetti/picklock/internal/store/mssql"     // registers SQL Server
	_ "github.com/agmonetti/picklock/internal/store/mysql"     // registers MySQL and MariaDB
	_ "github.com/agmonetti/picklock/internal/store/neo4j"     // registers Neo4j
	_ "github.com/agmonetti/picklock/internal/store/postgres"  // registers PostgreSQL
	_ "github.com/agmonetti/picklock/internal/store/redis"     // registers Redis
	_ "github.com/agmonetti/picklock/internal/store/sqlite"    // registers SQLite
	"github.com/agmonetti/picklock/internal/tui"
)

func main() {
	printLayout := flag.Bool("print-layout", false, "print the layout as text and exit (debug)")
	layoutW := flag.Int("width", 0, "force the terminal width for --print-layout (0 = detect)")
	layoutH := flag.Int("height", 0, "force the terminal height for --print-layout (0 = detect)")
	readOnly := flag.Bool("read-only", false, "open every connection read-only (all engines)")
	flag.Parse()
	if *printLayout {
		os.Exit(tui.PrintLayout(*layoutW, *layoutH))
	}
	if flag.NArg() > 1 {
		fmt.Fprintln(os.Stderr, "picklock: too many arguments — flags go before the DSN: picklock --read-only ./db.sqlite")
		os.Exit(1)
	}

	// optional DSN: `picklock ./db.sqlite` or `picklock postgres://...` connect
	// immediately, skipping the connection screen
	opts := tui.NewOpts{GlobalReadOnly: *readOnly}
	if flag.NArg() == 1 {
		cfg, err := conn.ParseDSN(flag.Arg(0))
		if err != nil {
			fmt.Fprintf(os.Stderr, "picklock: %v\n", err)
			os.Exit(1)
		}
		opts.InitialCfg = &cfg
	}

	// Cell motion reports mouse movement only while a button is held, which is
	// exactly what the pane resize drag needs.
	p := tea.NewProgram(tui.New(opts), tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "picklock: %v\n", err)
		os.Exit(1)
	}
}
