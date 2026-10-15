package mcp

import (
	"context"
	"fmt"
	"strings"
	"testing"

	mcpproto "github.com/mark3labs/mcp-go/mcp"

	"github.com/agmonetti/picklock/internal/conn"
	"github.com/agmonetti/picklock/internal/store"
	_ "github.com/agmonetti/picklock/internal/store/sqlite"
)

func TestQueryPreservesMCPRowLimit(t *testing.T) {
	cfg := conn.New(conn.DriverSQLite)
	cfg.Path = ":memory:"
	ds, err := store.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ds.Close() })

	statement := fmt.Sprintf("WITH RECURSIVE seq(n) AS (SELECT 1 UNION ALL SELECT n + 1 FROM seq WHERE n < %d) SELECT n FROM seq", maxQueryRows+1)
	req := mcpproto.CallToolRequest{
		Params: mcpproto.CallToolParams{
			Name:      "query",
			Arguments: map[string]any{"statement": statement},
		},
	}
	s := &Server{ds: ds}
	result, err := s.handleQuery(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("query failed: %+v", result)
	}
	if len(result.Content) != 1 {
		t.Fatalf("content = %+v, want one text result", result.Content)
	}
	text, ok := result.Content[0].(mcpproto.TextContent)
	if !ok {
		t.Fatalf("content type = %T, want text", result.Content[0])
	}
	if !strings.Contains(text.Text, fmt.Sprintf("\n%d\n", maxQueryRows)) ||
		strings.Contains(text.Text, fmt.Sprintf("\n%d\n", maxQueryRows+1)) {
		t.Fatalf("query did not preserve its %d-row cap: %s", maxQueryRows, text.Text)
	}
}
