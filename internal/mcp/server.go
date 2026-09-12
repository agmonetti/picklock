// Package mcp implements a Model Context Protocol server exposing
// picklock's multi-engine DataSource interface as MCP tools.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/agmonetti/picklock/internal/conn"
	"github.com/agmonetti/picklock/internal/store"
)

const (
	serverName    = "picklock"
	serverVersion = "0.1.0"
	maxBrowseRows = 100
	maxQueryRows  = 500
)

// Server holds the MCP server and the active database connection.
type Server struct {
	mcpServer *server.MCPServer
	readOnly  bool

	mu sync.RWMutex
	ds store.DataSource // nil until connect
}

// New creates a new MCP Server. If initialDSN is non-empty the server
// connects immediately; otherwise the client must call the connect tool.
func New(readOnly bool) *Server {
	s := &Server{readOnly: readOnly}

	s.mcpServer = server.NewMCPServer(
		serverName, serverVersion,
		server.WithToolCapabilities(false),
		server.WithRecovery(),
		server.WithInstructions(
			"picklock MCP server — a unified database browser for 9 engines "+
				"(SQLite, PostgreSQL, MySQL, MariaDB, SQL Server, MongoDB, Redis, Cassandra, Neo4j). "+
				"Use 'connect' to open a database, then list_objects / browse / inspect / query to interact with it.",
		),
	)

	s.registerTools()
	return s
}

// ConnectDSN connects to a database using a DSN string.
func (s *Server) ConnectDSN(dsn string) error {
	cfg, err := conn.ParseDSN(dsn)
	if err != nil {
		return err
	}
	cfg.ReadOnly = s.readOnly
	return s.connectCfg(cfg)
}

func (s *Server) connectCfg(cfg conn.ConnectionConfig) error {
	ds, err := store.New(cfg)
	if err != nil {
		return err
	}
	s.mu.Lock()
	old := s.ds
	s.ds = ds
	s.mu.Unlock()
	if old != nil {
		old.Close()
	}
	return nil
}

func (s *Server) getDS() (store.DataSource, error) {
	s.mu.RLock()
	ds := s.ds
	s.mu.RUnlock()
	if ds == nil {
		return nil, fmt.Errorf("not connected — call the 'connect' tool first")
	}
	return ds, nil
}

// Serve runs the MCP server on stdio (stdin/stdout) until ctx is cancelled.
func (s *Server) Serve() error {
	return server.ServeStdio(s.mcpServer)
}

// Close closes the active database connection.
func (s *Server) Close() {
	s.mu.Lock()
	ds := s.ds
	s.ds = nil
	s.mu.Unlock()
	if ds != nil {
		ds.Close()
	}
}

// --- tool registration ---

func (s *Server) registerTools() {
	s.mcpServer.AddTools(
		server.ServerTool{Tool: toolListConnections, Handler: s.handleListConnections},
		server.ServerTool{Tool: toolConnect, Handler: s.handleConnect},
		server.ServerTool{Tool: toolListObjects, Handler: s.handleListObjects},
		server.ServerTool{Tool: toolBrowse, Handler: s.handleBrowse},
		server.ServerTool{Tool: toolInspect, Handler: s.handleInspect},
		server.ServerTool{Tool: toolQuery, Handler: s.handleQuery},
	)
}

// --- tool definitions ---

var toolListConnections = mcp.NewTool("list_connections",
	mcp.WithDescription("List saved database connections from ~/.config/picklock/connections.json"),
)

var toolConnect = mcp.NewTool("connect",
	mcp.WithDescription("Connect to a database engine. Provide either a DSN string (e.g. postgres://user:pass@host:5432/db) or the name of a saved connection."),
	mcp.WithString("dsn", mcp.Description("Database connection string (DSN). Supports: sqlite path, postgres://, mysql://, mongodb://, redis://, cassandra://, neo4j://, sqlserver://")),
	mcp.WithString("saved_name", mcp.Description("Name of a saved connection from list_connections")),
)

var toolListObjects = mcp.NewTool("list_objects",
	mcp.WithDescription("List all objects in the connected database (tables, collections, keys, labels, depending on the engine)."),
)

var toolBrowse = mcp.NewTool("browse",
	mcp.WithDescription("Browse/paginate data from a database object (table rows, documents, key entries, graph nodes)."),
	mcp.WithString("object", mcp.Required(), mcp.Description("Object name (table, collection, key pattern, node label)")),
	mcp.WithNumber("page", mcp.Description("Page number (0-based, default 0)")),
	mcp.WithNumber("page_size", mcp.Description("Rows per page (default 50, max 100)")),
)

var toolInspect = mcp.NewTool("inspect",
	mcp.WithDescription("Inspect the structure/schema of a database object (columns, indexes, collection stats, key info, label properties)."),
	mcp.WithString("object", mcp.Required(), mcp.Description("Object name to inspect")),
)

var toolQuery = mcp.NewTool("query",
	mcp.WithDescription("Execute a query against the connected database. Supports SQL, MQL (MongoDB), RESP (Redis), CQL (Cassandra), and Cypher (Neo4j). Mutations are blocked in read-only mode."),
	mcp.WithString("statement", mcp.Required(), mcp.Description("The query statement to execute")),
)

// --- tool handlers ---

func (s *Server) handleListConnections(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	saved, err := conn.LoadSaved()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to load saved connections: %v", err)), nil
	}
	if len(saved) == 0 {
		return mcp.NewToolResultText("No saved connections found."), nil
	}
	data, _ := json.MarshalIndent(saved, "", "  ")
	return mcp.NewToolResultText(string(data)), nil
}

func (s *Server) handleConnect(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	dsn, _ := args["dsn"].(string)
	savedName, _ := args["saved_name"].(string)

	if dsn == "" && savedName == "" {
		return mcp.NewToolResultError("provide either 'dsn' or 'saved_name'"), nil
	}

	if savedName != "" {
		saved, err := conn.LoadSaved()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to load saved connections: %v", err)), nil
		}
		var found *conn.SavedConnection
		for i := range saved {
			if saved[i].Name == savedName {
				found = &saved[i]
				break
			}
		}
		if found == nil {
			return mcp.NewToolResultError(fmt.Sprintf("saved connection %q not found", savedName)), nil
		}
		cfg := found.ToConfig()
		cfg.ReadOnly = cfg.ReadOnly || s.readOnly
		if err := s.connectCfg(cfg); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("connection failed: %v", err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Connected to %s", cfg.Label())), nil
	}

	if err := s.ConnectDSN(dsn); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("connection failed: %v", err)), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("Connected via DSN")), nil
}

func (s *Server) handleListObjects(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ds, err := s.getDS()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	cat := ds.Catalog()
	items, err := cat.ListObjects(ctx)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to list objects: %v", err)), nil
	}
	if len(items) == 0 {
		return mcp.NewToolResultText(fmt.Sprintf("No %ss found.", cat.ItemNoun)), nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s (%d %ss):\n", cat.Title, len(items), cat.ItemNoun)
	for _, it := range items {
		b.WriteString("  - " + it.Name)
		if it.Badge != "" {
			fmt.Fprintf(&b, " [%s]", it.Badge)
		}
		if it.Metadata != "" {
			fmt.Fprintf(&b, " (%s)", it.Metadata)
		}
		b.WriteByte('\n')
	}
	return mcp.NewToolResultText(b.String()), nil
}

func (s *Server) handleBrowse(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ds, err := s.getDS()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	args := req.GetArguments()
	object, _ := args["object"].(string)
	if object == "" {
		return mcp.NewToolResultError("'object' is required"), nil
	}

	page := intArg(args, "page", 0)
	pageSize := intArg(args, "page_size", 50)
	if pageSize > maxBrowseRows {
		pageSize = maxBrowseRows
	}

	resp, err := ds.Browse(ctx, store.BrowseRequest{
		ObjectName: object,
		PageSize:   pageSize,
		Page:       page,
	})
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("browse failed: %v", err)), nil
	}

	text := formatDataView(resp.Data, resp.HasNext, resp.TotalCount, page)
	return mcp.NewToolResultText(text), nil
}

func (s *Server) handleInspect(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ds, err := s.getDS()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	object, _ := req.GetArguments()["object"].(string)
	if object == "" {
		return mcp.NewToolResultError("'object' is required"), nil
	}

	insp, err := ds.Inspect(ctx, object)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("inspect failed: %v", err)), nil
	}

	text := formatInspection(insp)
	return mcp.NewToolResultText(text), nil
}

func (s *Server) handleQuery(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ds, err := s.getDS()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	stmt, _ := req.GetArguments()["statement"].(string)
	if stmt == "" {
		return mcp.NewToolResultError("'statement' is required"), nil
	}

	qe := ds.Query()

	// Block mutations in read-only mode
	if ds.ReadOnly() && qe.IsMutation(stmt) {
		return mcp.NewToolResultError("mutation blocked: connection is read-only"), nil
	}

	data, err := qe.Execute(ctx, stmt, 0, maxQueryRows)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("query error: %v", err)), nil
	}
	if data == nil {
		return mcp.NewToolResultText("OK (no result)"), nil
	}

	text := formatDataView(data, false, -1, 0)
	return mcp.NewToolResultText(text), nil
}

// --- helpers ---

func intArg(args map[string]any, key string, def int) int {
	v, ok := args[key]
	if !ok {
		return def
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	}
	return def
}
