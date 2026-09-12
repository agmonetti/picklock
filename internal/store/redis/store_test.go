package redis

import (
	"context"
	"testing"
	"time"

	"github.com/agmonetti/picklock/internal/conn"
	"github.com/agmonetti/picklock/internal/store"
)

func TestRedis_Registered(t *testing.T) {
	cfg := conn.New(conn.DriverRedis)
	cfg.Host = "127.0.0.1"
	cfg.Port = 65533 // unopened port

	_, err := store.New(cfg)
	if err == nil {
		t.Fatal("expected connection error on unopened port")
	}
	if err.Error() == "unsupported driver: redis" {
		t.Fatal("DriverRedis not registered in store registry")
	}
}

func TestRedis_ParseCommandArgs(t *testing.T) {
	args, err := parseRedisCommandArgs(`SET "my key" 'my value with spaces'`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(args) != 3 {
		t.Fatalf("args len = %d, want 3", len(args))
	}
	if args[0] != "SET" || args[1] != "my key" || args[2] != "my value with spaces" {
		t.Errorf("args = %v", args)
	}

	args2, err := parseRedisCommandArgs(`HGETALL   users:1001 `)
	if err != nil || len(args2) != 2 || args2[0] != "HGETALL" || args2[1] != "users:1001" {
		t.Errorf("args2 = %v, err = %v", args2, err)
	}

	// Empty string in double quotes
	args3, err := parseRedisCommandArgs(`HSET instancia:101 estado "COMPLETADA" tarea_actual ""`)
	if err != nil {
		t.Fatalf("args3 error: %v", err)
	}
	if len(args3) != 6 {
		t.Fatalf("args3 len = %d, want 6 (including empty string)", len(args3))
	}
	expected3 := []any{"HSET", "instancia:101", "estado", "COMPLETADA", "tarea_actual", ""}
	for i, exp := range expected3 {
		if args3[i] != exp {
			t.Errorf("args3[%d] = %q, want %q", i, args3[i], exp)
		}
	}

	// Empty string in single quotes
	args4, err := parseRedisCommandArgs(`SET mykey ''`)
	if err != nil || len(args4) != 3 || args4[2] != "" {
		t.Errorf("args4 = %v, err = %v, want 3 args with empty string", args4, err)
	}

	// Escaped quotes inside double quotes
	args5, err := parseRedisCommandArgs(`SET msg "hello \"world\""`)
	if err != nil || len(args5) != 3 || args5[2] != `hello "world"` {
		t.Errorf("args5 = %v, err = %v", args5, err)
	}

	// Unclosed quotes
	_, err = parseRedisCommandArgs(`SET msg "unclosed string`)
	if err == nil {
		t.Error("expected error on unclosed quote")
	}
}

func TestRedis_FormatTTL(t *testing.T) {
	if s := formatTTL(-1 * time.Nanosecond); s != "-1 (no expiry)" {
		t.Errorf("formatTTL(-1) = %q", s)
	}
	if s := formatTTL(45 * time.Second); s != "45s" {
		t.Errorf("formatTTL(45s) = %q", s)
	}
	if s := formatTTL(125 * time.Second); s != "2m5s" {
		t.Errorf("formatTTL(125s) = %q", s)
	}
	if s := formatTTL(3700 * time.Second); s != "1h1m" {
		t.Errorf("formatTTL(3700s) = %q", s)
	}
}

func TestRedis_ExecutorIsMutationAndReadOnly(t *testing.T) {
	source := &RedisSource{readOnly: true}
	exec := &RedisExecutor{source: source}

	if !exec.IsMutation("SET foo bar") {
		t.Error("SET must be detected as mutation")
	}
	if !exec.IsMutation("HSET user name Alice") {
		t.Error("HSET must be detected as mutation")
	}
	if !exec.IsMutation("DEL foo") {
		t.Error("DEL must be detected as mutation")
	}
	if exec.IsMutation("GET foo") {
		t.Error("GET must not be detected as mutation")
	}
	if exec.IsMutation("HGETALL user") {
		t.Error("HGETALL must not be detected as mutation")
	}

	_, err := exec.Execute(context.Background(), "SET foo bar", 0, 10)
	if err == nil {
		t.Fatal("expected mutation to be blocked in read-only mode")
	}
}

func TestRedis_FormatResult(t *testing.T) {
	strRes := formatRedisResult("OK")
	tab, ok := strRes.(*store.TabularData)
	if !ok || len(tab.Rows) != 1 || tab.Rows[0][0] != "OK" {
		t.Errorf("string result = %v", strRes)
	}

	intRes := formatRedisResult(int64(42))
	tab2, ok2 := intRes.(*store.TabularData)
	if !ok2 || len(tab2.Rows) != 1 || tab2.Rows[0][0] != "42" {
		t.Errorf("int result = %v", intRes)
	}

	sliceRes := formatRedisResult([]any{"item1", "item2"})
	tab3, ok3 := sliceRes.(*store.TabularData)
	if !ok3 || len(tab3.Rows) != 2 {
		t.Errorf("slice result = %v", sliceRes)
	}
}

func TestRedis_SplitRedisCommands(t *testing.T) {
	t.Run("multiline commands without semicolon", func(t *testing.T) {
		raw := "HGETALL instancia:101\nSMEMBERS tareas_pendientes:rol:soporte"
		stmts := SplitRedisCommands(raw)
		if len(stmts) != 2 {
			t.Fatalf("len(stmts) = %d, want 2", len(stmts))
		}
		if stmts[0].Text != "HGETALL instancia:101" || stmts[0].Line != 0 {
			t.Errorf("stmt[0] = %+v, want 'HGETALL instancia:101' at line 0", stmts[0])
		}
		if stmts[1].Text != "SMEMBERS tareas_pendientes:rol:soporte" || stmts[1].Line != 1 {
			t.Errorf("stmt[1] = %+v, want 'SMEMBERS tareas_pendientes:rol:soporte' at line 1", stmts[1])
		}

		// Verify StatementAt selects the right statement based on cursor line
		if idx := store.StatementAt(stmts, 0); idx != 0 {
			t.Errorf("StatementAt(0) = %d, want 0", idx)
		}
		if idx := store.StatementAt(stmts, 1); idx != 1 {
			t.Errorf("StatementAt(1) = %d, want 1", idx)
		}
	})

	t.Run("semicolon separated on same line", func(t *testing.T) {
		raw := "SET foo bar; GET foo"
		stmts := SplitRedisCommands(raw)
		if len(stmts) != 2 {
			t.Fatalf("len(stmts) = %d, want 2", len(stmts))
		}
		if stmts[0].Text != "SET foo bar" || stmts[0].Line != 0 {
			t.Errorf("stmt[0] = %+v", stmts[0])
		}
		if stmts[1].Text != "GET foo" || stmts[1].Line != 0 {
			t.Errorf("stmt[1] = %+v", stmts[1])
		}
	})

	t.Run("multiline string in quotes", func(t *testing.T) {
		raw := "SET msg \"hello\nworld\"\nGET msg"
		stmts := SplitRedisCommands(raw)
		if len(stmts) != 2 {
			t.Fatalf("len(stmts) = %d, want 2", len(stmts))
		}
		if stmts[0].Text != "SET msg \"hello\nworld\"" || stmts[0].Line != 0 {
			t.Errorf("stmt[0] = %+v", stmts[0])
		}
		if stmts[1].Text != "GET msg" || stmts[1].Line != 2 {
			t.Errorf("stmt[1] = %+v", stmts[1])
		}
	})

	t.Run("comments ignored", func(t *testing.T) {
		raw := "# comment 1\nGET key1\n// comment 2\nGET key2\n-- comment 3\nGET key3"
		stmts := SplitRedisCommands(raw)
		if len(stmts) != 3 {
			t.Fatalf("len(stmts) = %d, want 3", len(stmts))
		}
		if stmts[0].Text != "GET key1" || stmts[0].Line != 1 {
			t.Errorf("stmt[0] = %+v", stmts[0])
		}
		if stmts[1].Text != "GET key2" || stmts[1].Line != 3 {
			t.Errorf("stmt[1] = %+v", stmts[1])
		}
		if stmts[2].Text != "GET key3" || stmts[2].Line != 5 {
			t.Errorf("stmt[2] = %+v", stmts[2])
		}
	})

	t.Run("inline comments", func(t *testing.T) {
		raw := "GET key1 # inline note\nGET key2"
		stmts := SplitRedisCommands(raw)
		if len(stmts) != 2 {
			t.Fatalf("len(stmts) = %d, want 2", len(stmts))
		}
		if stmts[0].Text != "GET key1" || stmts[0].Line != 0 {
			t.Errorf("stmt[0] = %+v", stmts[0])
		}
		if stmts[1].Text != "GET key2" || stmts[1].Line != 1 {
			t.Errorf("stmt[1] = %+v", stmts[1])
		}
	})
}
