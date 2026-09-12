package neo4j

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/agmonetti/picklock/internal/conn"
	"github.com/agmonetti/picklock/internal/store"
)

func init() {
	store.Register(conn.DriverNeo4j, New)
}

// Neo4jSource implements store.DataSource for Neo4j graph database.
type Neo4jSource struct {
	driver   neo4jdriver.DriverWithContext
	database string
	readOnly bool
}

// New creates and connects a Neo4jSource.
func New(cfg conn.ConnectionConfig) (store.DataSource, error) {
	uri := cfg.URI
	if uri == "" {
		host := cfg.Host
		if host == "" {
			host = "localhost"
		}
		port := cfg.Port
		if port <= 0 {
			port = 7687
		}
		uri = fmt.Sprintf("neo4j://%s:%d", host, port)
	}

	var auth neo4jdriver.AuthToken
	if cfg.User != "" {
		auth = neo4jdriver.BasicAuth(cfg.User, cfg.Password, "")
	} else {
		auth = neo4jdriver.NoAuth()
	}

	driver, err := neo4jdriver.NewDriverWithContext(uri, auth)
	if err != nil {
		return nil, fmt.Errorf("neo4j driver init: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := driver.VerifyConnectivity(ctx); err != nil {
		_ = driver.Close(ctx)
		return nil, fmt.Errorf("neo4j connect: %w", err)
	}

	dbName := cfg.Database
	if dbName == "" {
		dbName = "neo4j"
	}

	return &Neo4jSource{
		driver:   driver,
		database: dbName,
		readOnly: cfg.ReadOnly,
	}, nil
}

func (s *Neo4jSource) Driver() conn.Driver {
	return conn.DriverNeo4j
}

func (s *Neo4jSource) Version(ctx context.Context) (string, error) {
	session := s.driver.NewSession(ctx, neo4jdriver.SessionConfig{
		DatabaseName: s.database,
		AccessMode:   neo4jdriver.AccessModeRead,
	})
	defer session.Close(ctx)

	result, err := session.Run(ctx, "CALL dbms.components() YIELD name, versions, edition RETURN name, versions[0] AS version, edition", nil)
	if err != nil {
		return "Neo4j", nil
	}
	if result.Next(ctx) {
		record := result.Record()
		v, _ := record.Get("version")
		ed, _ := record.Get("edition")
		return fmt.Sprintf("Neo4j %v (%v)", v, ed), nil
	}
	return "Neo4j", nil
}

func (s *Neo4jSource) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.driver.Close(ctx)
}

func (s *Neo4jSource) ReadOnly() bool {
	return s.readOnly
}

func (s *Neo4jSource) Catalog() store.CatalogDescriptor {
	return store.CatalogDescriptor{
		Title:    "LABELS",
		ItemNoun: "label",
		ListObjects: func(ctx context.Context) ([]store.CatalogItem, error) {
			session := s.driver.NewSession(ctx, neo4jdriver.SessionConfig{
				DatabaseName: s.database,
				AccessMode:   neo4jdriver.AccessModeRead,
			})
			defer session.Close(ctx)

			result, err := session.Run(ctx, "CALL db.labels() YIELD label RETURN label ORDER BY label", nil)
			if err != nil {
				return nil, fmt.Errorf("list labels: %w", err)
			}

			var items []store.CatalogItem
			for result.Next(ctx) {
				record := result.Record()
				lbl, _ := record.Get("label")
				lblStr := fmt.Sprint(lbl)
				items = append(items, store.CatalogItem{
					Name: lblStr,
				})
			}
			return items, nil
		},
	}
}

func (s *Neo4jSource) Browse(ctx context.Context, req store.BrowseRequest) (store.BrowseResponse, error) {
	if req.ObjectName == "" {
		return store.BrowseResponse{}, errors.New("no label specified")
	}

	session := s.driver.NewSession(ctx, neo4jdriver.SessionConfig{
		DatabaseName: s.database,
		AccessMode:   neo4jdriver.AccessModeRead,
	})
	defer session.Close(ctx)

	label := req.ObjectName
	// 1. Total node count
	countCypher := fmt.Sprintf("MATCH (n:%s) RETURN count(n) AS total", label)
	countRes, err := session.Run(ctx, countCypher, nil)
	var total int64 = -1
	if err == nil && countRes.Next(ctx) {
		if tVal, ok := countRes.Record().Get("total"); ok {
			if tInt, ok := tVal.(int64); ok {
				total = tInt
			}
		}
	}

	limit := req.PageSize
	if limit <= 0 {
		limit = 50
	}
	skip := req.Page * limit

	// 2. Fetch nodes for the page
	fetchCypher := fmt.Sprintf("MATCH (n:%s) RETURN n SKIP %d LIMIT %d", label, skip, limit)
	fetchRes, err := session.Run(ctx, fetchCypher, nil)
	if err != nil {
		return store.BrowseResponse{}, fmt.Errorf("fetch nodes: %w", err)
	}

	var nodes []store.GraphNode
	for fetchRes.Next(ctx) {
		rec := fetchRes.Record()
		nVal, _ := rec.Get("n")
		node, ok := nVal.(neo4jdriver.Node)
		if !ok {
			continue
		}

		nodeID := fmt.Sprint(node.ElementId)
		if nodeID == "" {
			nodeID = strconv.FormatInt(node.Id, 10)
		}

		props := make(map[string]string)
		for k, v := range node.Props {
			props[k] = fmt.Sprint(v)
		}

		// Fetch incident edges for this node
		edgesCypher := `
			MATCH (n)-[r]-(m)
			WHERE elementId(n) = $nodeID OR id(n) = $legacyID
			RETURN type(r) AS rel_type, startNode(r) = n AS outgoing, elementId(m) AS target_id, labels(m) AS target_labels
			LIMIT 10
		`
		edgeRes, err := session.Run(ctx, edgesCypher, map[string]any{
			"nodeID":   node.ElementId,
			"legacyID": node.Id,
		})

		var incident []store.GraphEdgeSummary
		if err == nil {
			for edgeRes.Next(ctx) {
				eRec := edgeRes.Record()
				relType, _ := eRec.Get("rel_type")
				isOut, _ := eRec.Get("outgoing")
				targetID, _ := eRec.Get("target_id")
				targetLbls, _ := eRec.Get("target_labels")

				dir := "->"
				if isOutBool, ok := isOut.(bool); ok && !isOutBool {
					dir = "<-"
				}

				incident = append(incident, store.GraphEdgeSummary{
					Direction:     dir,
					Type:          fmt.Sprint(relType),
					TargetID:      fmt.Sprint(targetID),
					TargetSummary: fmt.Sprintf("(:%v)", targetLbls),
				})
			}
		}

		nodes = append(nodes, store.GraphNode{
			ID:         nodeID,
			Labels:     node.Labels,
			Properties: props,
			Incident:   incident,
		})
	}

	hasMore := (int64(skip) + int64(len(nodes))) < total
	nextCursor := ""
	if hasMore && len(nodes) > 0 {
		nextCursor = strconv.Itoa(req.Page + 1)
	}

	graphData := &store.GraphData{
		Nodes: nodes,
	}

	return store.BrowseResponse{
		Data:       graphData,
		HasNext:    hasMore,
		NextCursor: nextCursor,
		TotalCount: total,
	}, nil
}

func (s *Neo4jSource) Inspect(ctx context.Context, name string) (store.InspectionView, error) {
	session := s.driver.NewSession(ctx, neo4jdriver.SessionConfig{
		DatabaseName: s.database,
		AccessMode:   neo4jdriver.AccessModeRead,
	})
	defer session.Close(ctx)

	// Sample node properties from first 10 nodes
	propsMap := map[string]string{}
	sampleCypher := fmt.Sprintf("MATCH (n:%s) RETURN n LIMIT 10", name)
	sampleRes, err := session.Run(ctx, sampleCypher, nil)
	if err == nil {
		for sampleRes.Next(ctx) {
			if nVal, ok := sampleRes.Record().Get("n"); ok {
				if node, ok := nVal.(neo4jdriver.Node); ok {
					for k, v := range node.Props {
						propsMap[k] = fmt.Sprintf("%T", v)
					}
				}
			}
		}
	}

	var props []store.FieldSchema
	for k, t := range propsMap {
		props = append(props, store.FieldSchema{Name: k, Type: t})
	}

	// Fetch incident relationship types
	var relTypes []string
	relCypher := fmt.Sprintf("MATCH (:%s)-[r]-() RETURN DISTINCT type(r) AS rel_type LIMIT 20", name)
	relRes, err := session.Run(ctx, relCypher, nil)
	if err == nil {
		for relRes.Next(ctx) {
			if rVal, ok := relRes.Record().Get("rel_type"); ok {
				relTypes = append(relTypes, fmt.Sprint(rVal))
			}
		}
	}

	// Fetch indexes and constraints
	var indexes []store.Index
	var constraints []string
	ixRes, err := session.Run(ctx, "SHOW INDEXES YIELD name, labelsOrTypes, properties, uniqueness", nil)
	if err == nil {
		for ixRes.Next(ctx) {
			rec := ixRes.Record()
			ixName, _ := rec.Get("name")
			lbls, _ := rec.Get("labelsOrTypes")
			propsList, _ := rec.Get("properties")
			uniq, _ := rec.Get("uniqueness")

			// Check if this index belongs to our label
			if fmt.Sprint(lbls) == fmt.Sprintf("[%s]", name) {
				var cols []string
				if pSlice, ok := propsList.([]any); ok {
					for _, p := range pSlice {
						cols = append(cols, fmt.Sprint(p))
					}
				}
				isUniq := fmt.Sprint(uniq) == "UNIQUE"
				indexes = append(indexes, store.Index{
					Name:    fmt.Sprint(ixName),
					Columns: cols,
					Unique:  isUniq,
				})
			}
		}
	}

	return &store.GraphStructure{
		LabelName:     name,
		Properties:    props,
		Constraints:   constraints,
		Indexes:       indexes,
		Relationships: relTypes,
	}, nil
}

func (s *Neo4jSource) Query() store.QueryExecutor {
	return &Neo4jExecutor{source: s}
}

// Neo4jExecutor executes Cypher queries.
type Neo4jExecutor struct {
	source *Neo4jSource
}

func (e *Neo4jExecutor) Language() string {
	return "Cypher"
}

func (e *Neo4jExecutor) PromptTitle() string {
	return "CYPHER QUERY"
}

func (e *Neo4jExecutor) Placeholder() string {
	return "MATCH (n) RETURN n LIMIT 25;"
}

var cypherMutations = map[string]bool{
	"CREATE": true, "MERGE": true, "DELETE": true, "DETACH": true,
	"SET": true, "REMOVE": true, "DROP": true,
}

// IsMutation reports whether the statement writes data or schema. It scans the
// Cypher tokens, ignoring string literals, backtick-quoted identifiers and
// comments, so a read like MATCH (n) WHERE n.name='CREATE' RETURN n or
// RETURN n.offset is never misclassified, while MATCH (n) SET n.x=1 and
// MATCH (n) DETACH DELETE n still are.
func (e *Neo4jExecutor) IsMutation(stmt string) bool {
	for _, tok := range cypherTokens(stmt) {
		if cypherMutations[tok] {
			return true
		}
	}
	return false
}

// cypherTokens splits a Cypher statement into uppercase word tokens, skipping
// whitespace, comments, string literals and backtick-quoted identifiers.
func cypherTokens(s string) []string {
	var out []string
	i, n := 0, len(s)
	for i < n {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			i++
		case c == '/' && i+1 < n && s[i+1] == '/': // line comment
			for i < n && s[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < n && s[i+1] == '*': // block comment
			i += 2
			for i+1 < n && !(s[i] == '*' && s[i+1] == '/') {
				i++
			}
			i += 2
		case c == '\'' || c == '"': // string literal
			q := c
			i++
			for i < n {
				if s[i] == '\\' && i+1 < n {
					i += 2
					continue
				}
				if s[i] == q {
					i++
					break
				}
				i++
			}
		case c == '`': // quoted identifier
			i++
			for i < n && s[i] != '`' {
				i++
			}
			i++
		case isCypherWord(c):
			j := i
			for j < n && isCypherWord(s[j]) {
				j++
			}
			out = append(out, strings.ToUpper(s[i:j]))
			i = j
		default:
			i++
		}
	}
	return out
}

func isCypherWord(c byte) bool {
	switch {
	case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		return true
	case c == '_', c == '$':
		return true
	}
	return false
}

// SplitStatements splits a Cypher script into individual statements by semicolon (;),
// respecting single/double quoted strings, backtick-quoted identifiers, and comments
// (// line comments and /* */ block comments).
func (e *Neo4jExecutor) SplitStatements(buffer string) []store.Statement {
	var stmts []store.Statement
	var b strings.Builder
	line := 0
	startLine := 0
	started := false

	flush := func() {
		if t := strings.TrimSpace(b.String()); t != "" {
			stmts = append(stmts, store.Statement{Text: t, Line: startLine})
		}
		b.Reset()
		started = false
	}

	// 0 = code, 1 = single-quoted string, 2 = line comment (//), 3 = block comment (/* */),
	// 4 = double-quoted string, 5 = backtick-quoted identifier
	mode := 0
	n := len(buffer)
	for i := 0; i < n; i++ {
		c := buffer[i]
		if c == '\n' {
			line++
		}
		switch mode {
		case 1: // single-quoted string '...'
			b.WriteByte(c)
			if c == '\\' && i+1 < n {
				b.WriteByte(buffer[i+1])
				if buffer[i+1] == '\n' {
					line++
				}
				i++
			} else if c == '\'' {
				mode = 0
			}
		case 2: // line comment: // ...
			if c == '\n' {
				b.WriteByte(c)
				mode = 0
			}
		case 3: // block comment: /* ... */
			if c == '\n' {
				line++
			}
			if c == '*' && i+1 < n && buffer[i+1] == '/' {
				i++
				mode = 0
			}
		case 4: // double-quoted string "..."
			b.WriteByte(c)
			if c == '\\' && i+1 < n {
				b.WriteByte(buffer[i+1])
				if buffer[i+1] == '\n' {
					line++
				}
				i++
			} else if c == '"' {
				mode = 0
			}
		case 5: // backtick-quoted identifier `...`
			b.WriteByte(c)
			if c == '`' {
				if i+1 < n && buffer[i+1] == '`' {
					b.WriteByte(buffer[i+1])
					i++
				} else {
					mode = 0
				}
			}
		default: // code
			switch {
			case c == '\'':
				mode = 1
				if !started {
					startLine = line
					started = true
				}
				b.WriteByte(c)
			case c == '"':
				mode = 4
				if !started {
					startLine = line
					started = true
				}
				b.WriteByte(c)
			case c == '`':
				mode = 5
				if !started {
					startLine = line
					started = true
				}
				b.WriteByte(c)
			case c == '/' && i+1 < n && buffer[i+1] == '/':
				mode = 2
				i++
			case c == '/' && i+1 < n && buffer[i+1] == '*':
				mode = 3
				i++
				b.WriteByte(' ')
			case c == ';':
				flush()
			default:
				if !started && (c != ' ' && c != '\t' && c != '\r' && c != '\n') {
					startLine = line
					started = true
				}
				b.WriteByte(c)
			}
		}
	}
	flush()
	return stmts
}

// Execute runs a Cypher query. If a multi-statement buffer is provided,
// it executes the statement at line using SplitStatements / StatementAt.
func (e *Neo4jExecutor) Execute(ctx context.Context, buffer string, line int, maxRows int) (store.DataView, error) {
	trimmed := strings.TrimSpace(buffer)
	if trimmed == "" {
		return nil, errors.New("empty Cypher query")
	}

	stmts := e.SplitStatements(buffer)
	if len(stmts) == 0 {
		return nil, errors.New("empty Cypher query")
	}
	stmtIdx := 0
	if len(stmts) > 1 {
		stmtIdx = store.StatementAt(stmts, line)
	}
	targetQuery := stmts[stmtIdx].Text

	isMut := e.IsMutation(targetQuery)
	if e.source.readOnly && isMut {
		return nil, errors.New("mutations are blocked in read-only mode")
	}

	accessMode := neo4jdriver.AccessModeRead
	if isMut {
		accessMode = neo4jdriver.AccessModeWrite
	}

	session := e.source.driver.NewSession(ctx, neo4jdriver.SessionConfig{
		DatabaseName: e.source.database,
		AccessMode:   accessMode,
	})
	defer session.Close(ctx)

	result, err := session.Run(ctx, targetQuery, nil)
	if err != nil {
		return nil, fmt.Errorf("cypher error: %w", err)
	}

	var rows [][]string
	cols, err := result.Keys()
	if err != nil {
		cols = nil
	}

	var nodes []store.GraphNode
	truncated := false
	for result.Next(ctx) {
		if maxRows > 0 && len(rows) >= maxRows {
			truncated = true
			break
		}
		record := result.Record()
		row := make([]string, len(cols))
		for i, c := range cols {
			val, _ := record.Get(c)
			if n, ok := val.(neo4jdriver.Node); ok {
				nodeID := fmt.Sprint(n.ElementId)
				if nodeID == "" {
					nodeID = strconv.FormatInt(n.Id, 10)
				}
				props := make(map[string]string)
				for pk, pv := range n.Props {
					props[pk] = fmt.Sprint(pv)
				}
				nodes = append(nodes, store.GraphNode{
					ID:         nodeID,
					Labels:     n.Labels,
					Properties: props,
				})
				row[i] = fmt.Sprintf("(:%s %v)", strings.Join(n.Labels, ":"), props)
			} else {
				row[i] = fmt.Sprint(val)
			}
		}
		rows = append(rows, row)
	}

	summary, _ := result.Consume(ctx)
	if isMut && summary != nil {
		counters := summary.Counters()
		// Count structural changes only; a bare CREATE (node) reports 1 and a
		// property-only SET reports 0 affected (it still mutates schema/data).
		affected := counters.NodesCreated() + counters.NodesDeleted() +
			counters.RelationshipsCreated() + counters.RelationshipsDeleted()
		return &store.TabularData{
			Columns:   cols,
			Rows:      rows,
			Affected:  int64(affected),
			Truncated: truncated,
		}, nil
	}

	if len(nodes) > 0 && len(cols) == 1 {
		return &store.GraphData{
			Nodes: nodes,
		}, nil
	}

	return &store.TabularData{
		Columns:   cols,
		Rows:      rows,
		Affected:  -1, // read query
		TotalRows: -1,
		Truncated: truncated,
	}, nil
}
