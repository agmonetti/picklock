# 04 — Implementation: Technical Guide & Execution Details

## 1. Directory Structure

```
picklock/
├── internal/
│   ├── conn/
│   │   ├── conn.go          # Drivers enum, ports, ConnectionConfig, Label(), NeedsNetwork()
│   │   ├── parse.go         # ParseDSN: URIs (mongodb://, redis://, cassandra://, neo4j://...)
│   │   └── saved.go         # JSON persistence (~/.config/picklock/connections.json)
│   ├── store/
│   │   ├── store.go         # DataSource, CatalogDescriptor, DataView, InspectionView
│   │   ├── relational.go    # RelationalAdapter: wraps legacy Store to DataSource
│   │   ├── sql_split.go     # Verbatim SQL statement splitter & keyword tokenizer
│   │   ├── errors.go        # ErrUnsupportedDriver, ErrConnection, ErrObjectNotFound
│   │   ├── sqlite/          # SQLite Store (relational)
│   │   ├── postgres/        # PostgreSQL Store (relational)
│   │   ├── mysql/           # MySQL / MariaDB Store (relational)
│   │   ├── mssql/           # SQL Server Store (relational)
│   │   ├── mongo/           # MongoDB Store (Document)
│   │   ├── redis/           # Redis Store (Key-Value & Structures)
│   │   ├── cassandra/       # Cassandra Store (Wide-Column)
│   │   └── neo4j/           # Neo4j Store (Graph)
│   ├── browser/
│   │   └── browser.go       # CatalogItem navigation, DataView browsing, cursor stack
│   ├── editor/
│   │   ├── editor.go        # Multi-paradigm query execution, history ring buffer
│   │   ├── history.go       # In-memory history
│   │   └── history_file.go  # Persistent history (~/.config/picklock/history.json)
│   ├── export/
│   │   └── export.go        # Multi-paradigm serializers (CSV/JSON for DataView)
│   └── tui/
│       ├── model.go         # Main bubbletea model
│       ├── update.go        # Message dispatching, paradigm-aware reloads
│       ├── view.go          # Top-level view composition
│       ├── session.go       # Session lifecycle and connection setup
│       ├── detail.go        # Full JSON / node property & relationship detail view
│       └── screens/
│           ├── connect.go   # Engine-aware connection form
│           ├── workspace.go # Adaptive 3-pane layout
│           ├── browser.go   # DataView rendering (Tabular, Document, KV, Graph)
│           ├── editor.go    # DataView editor result rendering
│           └── structure.go # InspectionView rendering
```

---

## 2. Pagination Implementation & Cursor Stack

Different database paradigms use fundamentally different pagination protocols:
- **Relational**: Keyset `WHERE pk > ? ORDER BY pk LIMIT n` (fallback `LIMIT/OFFSET`).
- **MongoDB**: BSON cursor traversal or skip/limit over indexed fields.
- **Redis**: Key catalog uses cursor integers (`SCAN cursor COUNT 100`); data structures use indices (`LRANGE 0 49`, `ZRANGE 0 49 WITHSCORES`) or sub-cursors (`HSCAN`, `SSCAN`).
- **Cassandra**: Driver native `gocql.Iter.PageState()` binary tokens.
- **Neo4j**: Cypher `SKIP (page * size) LIMIT (size + 1)`.

### Preserving Backward Navigation (`PrevPage`)
To ensure reliable backward traversal across cursor-based engines:
1. `browser.Browser` maintains a cursor stack `cur []string`.
2. Advancing forward (`NextPage`) pushes the current page boundary cursor to `cur`.
3. Navigating backward (`PrevPage`) pops from `cur` and re-queries forward from the previous cursor position.

---

## 3. Multi-Paradigm Export Strategy (`internal/export`)

The export subsystem (`WriteCSV`, `WriteJSON`) operates on `DataView`:

| DataView | Format: JSON (`.json`) | Format: CSV (`.csv`) |
|---|---|---|
| **`TabularData`** | JSON array of column-ordered objects (`null` for SQL NULL). | RFC 4180 CSV with column header row. |
| **`DocumentData`** | JSON array of verbatim indented JSON documents. | Flattened top-level properties (if rectangular). |
| **`KeyValueData`** | JSON object (for hashes) or JSON array (for lists/sets/zsets). | CSV table with `Index,Value,Extra` columns. |
| **`GraphData`** | JSON object with `{ "nodes": [...], "edges": [...] }`. | CSV table of nodes and properties. |
| **`RawTextData`** | JSON `{ "title": ..., "text": ... }`. | Plain raw text output. |

---

## 4. Post-Write Reload Strategy

After an editor query modifies schema or data (`isMutation == true`), the TUI triggers `browser.Reload(ctx, ds)`.

The reload strategy adapts to each engine:
- **Relational / Cassandra**: Re-fetches catalog and current table page.
- **Redis**: Re-scans keys using `SCAN` and re-fetches active key entries (avoids expensive `COUNT(*)`).
- **MongoDB**: Re-lists collections and re-fetches current collection documents.
- **Neo4j**: Re-lists labels and re-fetches current label nodes.

---

## 5. Read-Only Enforcement Tiers

`picklock` implements a clear, 3-tier read-only hierarchy:

1. **Tier 1 — Server-Enforced (Native Session Mode)**:
    - SQLite: Opens file with `mode=ro`.
    - PostgreSQL: Connects with `options=-cdefault_transaction_read_only=on`.
    - MySQL / MariaDB: Pins connection with `SET SESSION TRANSACTION READ ONLY`.
    - Neo4j: Uses `neo4j.WithSessionMode(neo4j.AccessModeRead)`. The Neo4j server strictly rejects writes.
2. **Tier 2 — Client-Blocked (Command & Statement Guard)**:
    - Redis: Picklock's client-side executor checks the command keyword against a mutation blacklist (`SET`, `DEL`, `HDEL`, `LPUSH`, `LPOP`, `SADD`, `SREM`, `ZADD`, `ZREM`, `FLUSHDB`, `FLUSHALL`, `MSET`, `RENAME`, etc.) and blocks execution immediately with `"operation rejected: connection is read-only"`.
    - Cassandra: Picklock's CQL executor blocks `INSERT`, `UPDATE`, `DELETE`, `DROP`, `TRUNCATE`, `ALTER`.
    - SQL Server: Picklock's SQL executor blocks `INSERT`, `UPDATE`, `DELETE`, `CREATE`, `DROP` etc. via `IsSQLWrite` when `read_only` is set.
3. **Tier 3 — Advisory Notice**:
    - Cassandra: Shows amber notice: `read-only is client-guarded on cassandra — connect with a read-only role for full server enforcement`.
