package neo4j

import (
	"context"
	"strings"
	"testing"

	"github.com/agmonetti/picklock/internal/conn"
	"github.com/agmonetti/picklock/internal/store"
)

func TestNeo4j_Registered(t *testing.T) {
	cfg := conn.New(conn.DriverNeo4j)
	cfg.Host = "127.0.0.1"
	cfg.Port = 65531 // unopened port

	_, err := store.New(cfg)
	if err == nil {
		t.Fatal("expected connection error on unopened port")
	}
	if err.Error() == "unsupported driver: neo4j" {
		t.Fatal("DriverNeo4j not registered in store registry")
	}
}

func TestNeo4j_ExecutorIsMutationAndReadOnly(t *testing.T) {
	source := &Neo4jSource{readOnly: true}
	exec := &Neo4jExecutor{source: source}

	if !exec.IsMutation("CREATE (n:Person {name: 'Alice'})") {
		t.Error("CREATE must be detected as mutation")
	}
	if !exec.IsMutation("MATCH (n:Person) DETACH DELETE n") {
		t.Error("DETACH DELETE must be detected as mutation")
	}
	if !exec.IsMutation("MERGE (n:Person {id: 1})") {
		t.Error("MERGE must be detected as mutation")
	}
	if !exec.IsMutation("MATCH (n) SET n.online = true RETURN n") {
		t.Error("MATCH ... SET ... RETURN must be detected as mutation")
	}
	if !exec.IsMutation("MATCH (n) REMOVE n:Old RETURN n") {
		t.Error("REMOVE must be detected as mutation")
	}
	if exec.IsMutation("MATCH (n:Person) RETURN n") {
		t.Error("MATCH RETURN must not be detected as mutation")
	}
	// mutation words inside strings, substrings, or property names are not writes
	if exec.IsMutation("MATCH (n) WHERE n.name='CREATE' RETURN n") {
		t.Error("'CREATE' inside a string literal must not be a mutation")
	}
	if exec.IsMutation("MATCH (n) RETURN n.offset") {
		t.Error("property name containing 'set' must not be a mutation")
	}
	if exec.IsMutation("RETURN 'DELETE' AS label") {
		t.Error("'DELETE' inside a string literal must not be a mutation")
	}

	_, err := exec.Execute(context.Background(), "CREATE (n:Person)", 0, 10)
	if err == nil {
		t.Fatal("expected mutation to be blocked in read-only mode")
	}
}

func TestNeo4j_CypherTokens(t *testing.T) {
	got := cypherTokens(`MATCH (n:Person) WHERE n.name='CREATE' RETURN n.offset, "DETACH"`)
	want := []string{"MATCH", "N", "PERSON", "WHERE", "N", "NAME", "RETURN", "N", "OFFSET"}
	if len(got) != len(want) {
		t.Fatalf("tokens = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("token[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestNeo4j_LanguageAndTitle(t *testing.T) {
	exec := &Neo4jExecutor{}
	if exec.Language() != "Cypher" {
		t.Errorf("Language = %q, want Cypher", exec.Language())
	}
	if exec.PromptTitle() != "CYPHER QUERY" {
		t.Errorf("PromptTitle = %q, want CYPHER QUERY", exec.PromptTitle())
	}
}

func TestNeo4j_SplitStatements(t *testing.T) {
	exec := &Neo4jExecutor{}

	// Single statement without semicolon
	stmts := exec.SplitStatements("MATCH (n) RETURN n")
	if len(stmts) != 1 || stmts[0].Text != "MATCH (n) RETURN n" || stmts[0].Line != 0 {
		t.Errorf("single statement mismatch: %+v", stmts)
	}

	// Multiple statements separated by semicolon on different lines
	buffer := `match (n) return n;
MATCH (os {name: 'Oscar'}) SET os: Person;
RETURN 1`
	stmts = exec.SplitStatements(buffer)
	if len(stmts) != 3 {
		t.Fatalf("expected 3 statements, got %d: %+v", len(stmts), stmts)
	}
	if stmts[0].Text != "match (n) return n" || stmts[0].Line != 0 {
		t.Errorf("stmt 0 mismatch: %+v", stmts[0])
	}
	if stmts[1].Text != "MATCH (os {name: 'Oscar'}) SET os: Person" || stmts[1].Line != 1 {
		t.Errorf("stmt 1 mismatch: %+v", stmts[1])
	}
	if stmts[2].Text != "RETURN 1" || stmts[2].Line != 2 {
		t.Errorf("stmt 2 mismatch: %+v", stmts[2])
	}

	// Semicolons inside string literals and backtick identifiers
	complexBuf := `// Query 1
MATCH (n {val: "hello; world"})
WHERE n.name = 'it\'s; fine'
RETURN n;

/* Block comment with ; inside */
MATCH (n:` + "`" + `Special;Label` + "`" + `)
RETURN n`
	stmts = exec.SplitStatements(complexBuf)
	if len(stmts) != 2 {
		t.Fatalf("expected 2 statements with embedded semicolons, got %d: %+v", len(stmts), stmts)
	}
	if !strings.Contains(stmts[0].Text, `"hello; world"`) || !strings.Contains(stmts[0].Text, `'it\'s; fine'`) {
		t.Errorf("stmt 0 should preserve strings with semicolons: %q", stmts[0].Text)
	}
	if !strings.Contains(stmts[1].Text, "`Special;Label`") {
		t.Errorf("stmt 1 should preserve backtick identifier: %q", stmts[1].Text)
	}
}
