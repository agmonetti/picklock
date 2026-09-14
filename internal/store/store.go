// Package store defines the DataSource interface and semantic DataView models
// implemented by all database engines.
package store

import (
	"context"
	"fmt"
	"sync"

	"github.com/agmonetti/picklock/internal/conn"
)

// Column describes a column of a relational or wide-column table.
type Column struct {
	Name       string
	Type       string
	NotNull    bool
	Default    string
	PK         bool // Primary / Partition Key
	Clustering bool // Cassandra Clustering Column (CC)
}

// ForeignKey describes one referential constraint, preserving column order
// for composite keys.
type ForeignKey struct {
	Name              string
	TableSchema       string
	Table             string
	Columns           []string
	ReferencedSchema  string
	ReferencedTable   string
	ReferencedColumns []string
	UpdateRule        string
	DeleteRule        string
}

// Index describes an index of a table or collection.
type Index struct {
	Name    string
	Columns []string
	Unique  bool
}

// Result is the legacy outcome of a query kept for internal relational stores.
type Result struct {
	Columns   []string
	Rows      [][]string
	Affected  int64 // rows affected by an Exec; -1 when not applicable (read query)
	Truncated bool
	Nulls     [][]bool
}

// --- Multi-Paradigm DataView Results ---

// DataView is the presentation contract for all query and browse outcomes.
type DataView interface {
	dataView()
	Summary() string
	IsEmpty() bool
}

// TabularData represents rectangular data (relational rows, Cassandra CQL rows, SQL query projections).
type TabularData struct {
	Columns   []string
	Rows      [][]string
	Nulls     [][]bool
	Affected  int64 // >= 0 for write queries, -1 for read queries
	Truncated bool
	TotalRows int64 // -1 if unknown / unbounded
}

func (t *TabularData) dataView() {}
func (t *TabularData) Summary() string {
	if t.Affected >= 0 {
		noun := "rows"
		if t.Affected == 1 {
			noun = "row"
		}
		return fmt.Sprintf("%d %s affected", t.Affected, noun)
	}
	noun := "rows"
	if len(t.Rows) == 1 {
		noun = "row"
	}
	if t.TotalRows >= 0 {
		return fmt.Sprintf("%d of %d %s", len(t.Rows), t.TotalRows, noun)
	}
	return fmt.Sprintf("%d %s", len(t.Rows), noun)
}
func (t *TabularData) IsEmpty() bool {
	return len(t.Rows) == 0 && t.Affected < 0
}

// DocumentItem represents a single document (e.g. MongoDB BSON/JSON document).
type DocumentItem struct {
	ID      string // e.g. ObjectId("64a...")
	Summary string // Compact preview string
	RawJSON string // Formatted indented JSON
}

// DocumentData represents a list of hierarchical JSON/BSON documents.
type DocumentData struct {
	Documents []DocumentItem
	TotalDocs int64 // -1 if unknown
}

func (d *DocumentData) dataView() {}
func (d *DocumentData) Summary() string {
	noun := "documents"
	if len(d.Documents) == 1 {
		noun = "document"
	}
	if d.TotalDocs >= 0 {
		return fmt.Sprintf("%d of %d %s", len(d.Documents), d.TotalDocs, noun)
	}
	return fmt.Sprintf("%d %s", len(d.Documents), noun)
}
func (d *DocumentData) IsEmpty() bool {
	return len(d.Documents) == 0
}

// KVEntry represents an entry inside a key (Hash field, List element, Set member, ZSet item).
type KVEntry struct {
	Index string // Field name, list index (0, 1..), or rank
	Value string // Field value, element, or member
	Extra string // Optional ZSet score or stream timestamp
}

// KeyValueData represents Redis key structures.
type KeyValueData struct {
	Key      string
	Type     string            // "string", "hash", "list", "set", "zset", "stream"
	TTL      string            // "-1 (no TTL)", "847s", "expired"
	Metadata map[string]string // Memory, encoding, length
	Entries  []KVEntry
}

func (k *KeyValueData) dataView() {}
func (k *KeyValueData) Summary() string {
	if k.Type == "string" {
		return fmt.Sprintf("string (%s)", k.TTL)
	}
	return fmt.Sprintf("%s (%d entries, TTL: %s)", k.Type, len(k.Entries), k.TTL)
}
func (k *KeyValueData) IsEmpty() bool {
	return len(k.Entries) == 0 && k.Key == ""
}

// GraphNode represents a node in a graph.
type GraphNode struct {
	ID         string
	Labels     []string
	Properties map[string]string
	Incident   []GraphEdgeSummary
}

// GraphEdge represents a directed relationship between two nodes.
type GraphEdge struct {
	ID         string
	Type       string
	StartNode  string
	EndNode    string
	Properties map[string]string
}

// GraphEdgeSummary represents an incident relationship attached to a node.
type GraphEdgeSummary struct {
	Direction     string // "->" or "<-"
	Type          string // e.g. "PURCHASED"
	TargetID      string // e.g. "Product #42"
	TargetSummary string
}

// GraphData represents nodes and relationships.
type GraphData struct {
	Nodes []GraphNode
	Edges []GraphEdge
}

func (g *GraphData) dataView() {}
func (g *GraphData) Summary() string {
	nodeNoun := "nodes"
	if len(g.Nodes) == 1 {
		nodeNoun = "node"
	}
	edgeNoun := "edges"
	if len(g.Edges) == 1 {
		edgeNoun = "edge"
	}
	return fmt.Sprintf("%d %s, %d %s", len(g.Nodes), nodeNoun, len(g.Edges), edgeNoun)
}
func (g *GraphData) IsEmpty() bool {
	return len(g.Nodes) == 0 && len(g.Edges) == 0
}

// RawTextData represents plain command output, diagnostics, or status text.
type RawTextData struct {
	Title string
	Text  string
}

func (r *RawTextData) dataView() {}
func (r *RawTextData) Summary() string {
	if r.Title != "" {
		return r.Title
	}
	return "status output"
}
func (r *RawTextData) IsEmpty() bool {
	return r.Text == ""
}

// --- Inspection Views ---

// InspectionView represents the structural description or schema of an entity.
type InspectionView interface {
	inspectionView()
	Title() string
}

// RelationalStructure describes columns, indexes, and relationships of a table.
type RelationalStructure struct {
	Columns       []Column
	Indexes       []Index
	ForeignKeys   []ForeignKey
	ReferencedBy  []ForeignKey
	RelationTable bool
}

func (r *RelationalStructure) inspectionView() {}
func (r *RelationalStructure) Title() string   { return "Columns, Indexes & Relationships" }

// FieldSchema represents a field name and inferred type.
type FieldSchema struct {
	Name string
	Type string
}

// DocumentStructure describes collection stats, indexes, and inferred fields.
type DocumentStructure struct {
	CollectionName string
	DocCount       int64
	AvgSize        int64
	TotalSize      int64
	IndexSize      int64
	Indexes        []Index
	SampleFields   []FieldSchema
}

func (d *DocumentStructure) inspectionView() {}
func (d *DocumentStructure) Title() string   { return fmt.Sprintf("Collection: %s", d.CollectionName) }

// KeyValueStructure describes Redis key metadata and server stats.
type KeyValueStructure struct {
	Key        string
	Type       string
	TTL        string
	Encoding   string
	MemUsage   int64
	Length     int64
	ServerInfo map[string]string
}

func (k *KeyValueStructure) inspectionView() {}
func (k *KeyValueStructure) Title() string   { return fmt.Sprintf("Key Info: %s", k.Key) }

// CatalogItemKind identifies the semantic kind of a catalog object.
type CatalogItemKind string

const (
	CatalogItemTable    CatalogItemKind = "table"
	CatalogItemRelation CatalogItemKind = "relation"
)

// CatalogItem is an object in the navigation sidebar.
type CatalogItem struct {
	Name     string
	Kind     CatalogItemKind
	Group    string
	Badge    string // e.g. "hash", "list", "set", "zset", "relation"
	Metadata string // e.g. count or size
}

// GraphStructure describes Node Label schema, properties, constraints, and relationships.
type GraphStructure struct {
	LabelName     string
	Properties    []FieldSchema
	Constraints   []string
	Indexes       []Index
	Relationships []string
}

func (g *GraphStructure) inspectionView() {}
func (g *GraphStructure) Title() string   { return fmt.Sprintf("Label: %s", g.LabelName) }

// --- Catalog & Browsing Models ---

// CatalogDescriptor defines the sidebar title and object discovery mechanism.
type CatalogDescriptor struct {
	Title       string // e.g. "TABLES", "COLLECTIONS", "KEYS", "LABELS"
	ItemNoun    string // e.g. "table", "collection", "key", "label"
	ListObjects func(ctx context.Context) ([]CatalogItem, error)
}

// BrowseRequest expresses generic browsing pagination.
type BrowseRequest struct {
	ObjectName string
	PageSize   int
	Page       int
	Cursor     string
}

// BrowseResponse returns the page of data and cursor advancement.
type BrowseResponse struct {
	Data       DataView
	HasNext    bool
	NextCursor string
	PrevCursor string
	TotalCount int64 // -1 if unknown or expensive
}

// QueryExecutor encapsulates engine-specific query execution and syntax.
type QueryExecutor interface {
	Language() string
	PromptTitle() string
	Placeholder() string
	Execute(ctx context.Context, buffer string, line int, maxRows int) (DataView, error)
	IsMutation(statement string) bool
}

// StatementSplitter is an optional interface that a QueryExecutor can implement
// to provide engine-specific statement segmentation (e.g. Redis CLI splitting by newline).
type StatementSplitter interface {
	SplitStatements(buffer string) []Statement
}

// DataSource is the primary contract implemented by all database engines.
type DataSource interface {
	Driver() conn.Driver
	Version(ctx context.Context) (string, error)
	Close() error
	ReadOnly() bool

	Catalog() CatalogDescriptor
	Browse(ctx context.Context, req BrowseRequest) (BrowseResponse, error)
	Inspect(ctx context.Context, objectName string) (InspectionView, error)
	Query() QueryExecutor
}

// --- Legacy Store Interface (Kept for internal relational implementations) ---

// Store is the internal legacy relational interface preserved for relational engines.
type Store interface {
	Driver() string
	Version() (string, error)
	Close() error

	Tables() ([]string, error)
	Columns(table string) ([]Column, error)
	Indexes(table string) ([]Index, error)
	ForeignKeysContext(ctx context.Context) ([]ForeignKey, error)

	Query(sql string) (*Result, error)
	Exec(sql string) (int64, error)
	QueryContext(ctx context.Context, sql string) (*Result, error)
	ExecContext(ctx context.Context, sql string) (int64, error)
	QueryContextMax(ctx context.Context, sql string, max int) (*Result, error)

	CountTable(table string) (int, error)
	SelectTablePage(table string, limit, offset int) (*Result, error)
	SelectTableKeysetPage(table, key string, limit int, cursor string) (*Result, error)

	CountTableContext(ctx context.Context, table string) (int, error)
	SelectTablePageContext(ctx context.Context, table string, limit, offset int) (*Result, error)
	SelectTableKeysetPageContext(ctx context.Context, table, key string, limit int, cursor string) (*Result, error)
}

// --- Registry and Factory ---

// DataSourceConstructor creates a DataSource for an engine.
type DataSourceConstructor func(conn.ConnectionConfig) (DataSource, error)

// LegacyStoreConstructor creates a legacy Store for an engine.
type LegacyStoreConstructor func(conn.ConnectionConfig) (Store, error)

var (
	registryMu sync.RWMutex
	registry   = map[conn.Driver]DataSourceConstructor{}
)

// Register registers an engine's DataSource constructor.
func Register(d conn.Driver, c DataSourceConstructor) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[d] = c
}

// RegisterLegacy registers a legacy relational Store constructor by wrapping it in RelationalAdapter.
func RegisterLegacy(d conn.Driver, c LegacyStoreConstructor) {
	Register(d, func(cfg conn.ConnectionConfig) (DataSource, error) {
		st, err := c(cfg)
		if err != nil {
			return nil, err
		}
		return NewRelationalAdapter(st, cfg), nil
	})
}

// New creates a DataSource for the given configuration.
func New(cfg conn.ConnectionConfig) (DataSource, error) {
	registryMu.RLock()
	c, ok := registry[cfg.Driver]
	registryMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedDriver, cfg.Driver)
	}
	return c(cfg)
}
