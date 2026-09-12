<p align="center">
  <img src="assets/icon.png" alt="picklock" width="240">
</p>

<h1 align="center">picklock</h1>

<p align="center">
  A TUI data browser for people who don't leave the terminal.<br>
  <strong>Relational · Document · Key-Value · Wide-Column · Graph</strong><br>
  SQLite · PostgreSQL · MySQL · MariaDB · SQL Server · MongoDB · Redis · Cassandra · Neo4j
</p>

---

<p align="center">
  <img src="assets/demo.png" alt="picklock demo" width="85%">
</p>

Browse tables, documents, key-values, and graph structures, run queries and inspect schemas — all from the keyboard, all in one window. No Electron, no browser tab, no mouse required.

> **First time?** `picklock` connects to databases that already exist — it doesn't start servers.
> See **[USAGE.md](USAGE.md)** for a step-by-step guide: create a test database, connect and run your first query.
>
> **Try it right now:** `go run ./cmd/demo` creates `demo.db` with 20 tables and
> a few thousand rows each (no server needed). With `docker compose up -d` first,
> `go run ./cmd/demo --all` seeds the same dataset into PostgreSQL, MySQL,
> MariaDB, SQL Server, MongoDB, Redis, Cassandra, and Neo4j too.

## Supported Data Paradigms & Engines

| Paradigm | Engines | Catalog Unit | Data View | Structure Inspector | Query Language |
|---|---|---|---|---|---|
| **Relational** | SQLite, PostgreSQL, MySQL, MariaDB, SQL Server | Tables | Table rows (keyset pagination) | Columns (PK, NN, DEF), Indexes | SQL |
| **Document** | MongoDB | Collections | BSON/JSON Documents, IDs, Summaries | Collection stats, Indexes, Inferred Fields | MQL (`db.col.find()`, JSON) |
| **Key-Value** | Redis | Keys (with badge by type) | Strings, Hashes, Lists, Sets, ZSets | Key info, TTL, Memory, Server metrics | RESP Commands (`GET`, `HGETALL`) |
| **Wide-Column** | Apache Cassandra / ScyllaDB | Tables | Column rows (page state cursors) | Partition Keys (PK), Clustering Columns (CC) | CQL |
| **Graph** | Neo4j | Node Labels | Nodes, Labels, Properties, Incident Edges | Label schema, Properties, Indexes, Relationships | Cypher |

## Install

Requires Go 1.26.6+.

```bash
go install github.com/agmonetti/picklock@latest
```

Or build from source:

```bash
git clone https://github.com/agmonetti/picklock
cd picklock
go build -o picklock ./cmd/picklock
```

## Usage

```bash
picklock
```

The connection screen opens. Pick the engine with `←`/`→`, fill in the fields and press `Enter`. For SQLite you only need the file path.

To skip the connection screen, pass a DSN directly:

```bash
# Relational
picklock ./app.db                                         # SQLite file
picklock postgres://postgres:postgres@localhost:5432/test  # PostgreSQL
picklock mysql://root:root@localhost:3306/test            # MySQL
picklock 'sqlserver://sa:Str0ng!Passw0rd@localhost:1433?database=master'

# Non-Relational
picklock mongodb://localhost:27017/test                   # MongoDB
picklock redis://localhost:6379/0                         # Redis
picklock cassandra://localhost:9042/picklock_demo             # Cassandra
picklock neo4j://neo4j:password@localhost:7687/neo4j     # Neo4j

# Read-only enforcement
picklock --read-only postgres://user:pass@host:5432/mydb
picklock --read-only redis://localhost:6379/0
```

## What you get

- **Single-window adaptive layout** — sidebar (catalog), main data viewer (tables, documents, key-values, graph nodes) and editor always visible, always in sync.
- **Multi-paradigm pagination** — relational keyset pagination, MongoDB skip/limit pages, Redis SCAN/paging, Cassandra page states, and Cypher skip/limits.
- **Native Query Editor** — executes SQL, MQL, RESP, CQL, and Cypher with statement segmentation (delimited by `;` for SQL/CQL/Cypher and newlines for Redis) targeting the statement under the cursor, with history of your last 100 queries (`~/.config/picklock/history.json`).
- **Auto-refresh** — after any write query the catalog and active item reload automatically in the background.
- **Structure Inspector (`i`)** — view columns and indexes (relational), collection stats and inferred schema (MongoDB), key memory and server stats (Redis), Partition Keys & Clustering Columns (Cassandra), or node label schemas & relationship types (Neo4j).
- **Detail View (`v`)** — full values for table rows, pretty-printed JSON for documents, full key-value entries, and graph node properties + incident edges.
- **Universal Export (`Alt+E`)** — export tabular queries, documents, and key structures directly to CSV or formatted JSON.
- **Saved connections** — stored securely in `~/.config/picklock/connections.json`.
- **Read-only enforcement** across all 9 engines.

## Shortcuts

| Key | Action |
|---|---|
| `Tab` | Cycle focus: sidebar → main view → editor |
| `Alt+1` / `Alt+2` / `Alt+3` | Jump directly to sidebar / main view / editor |
| `↑↓` / `k j` | Navigate rows, items, documents, keys, or nodes |
| `Enter` (sidebar) | Open item and focus main panel |
| `PgUp` / `PgDn` | Change page / cursor forward or backward |
| `i` | Structure inspection |
| `v` | Detail view (full row, JSON document, KV entries, graph node) |
| `r` | Refresh active item and catalog |
| `Alt+E` | Export result / current page to CSV or JSON |
| `Alt+C` / `Ctrl+Y` | Copy active query to clipboard |
| `Alt+B` | Toggle sidebar |
| `Alt+M` | Toggle main data view |
| `Alt+Q` | Toggle query editor |
| `Alt+Z` | Zoom / maximize active pane |
| `Ctrl+R` | Run query (SQL / MQL / RESP / CQL / Cypher) |
| `Esc` | Cancel running query or return from detail/structure |
| `Ctrl+L` | Clear editor |
| `↑↓` (editor) | Query history |
| `←→` (main) / `Alt+←→` (editor) | Scroll table columns horizontally |
| `Ctrl+N` | New connection |
| `Ctrl+S` | Save connection |
| `Ctrl+P` | Settings (query timeout) |
| `?` | Help / Cheatsheet |
| `Ctrl+C` / `q` | Quit |
| click drag (editor) | Select & auto-copy query text |
| right-click drag | Resize panes |
| scroll wheel / swipe | Scroll pane / table columns under cursor |

## MCP Server (Model Context Protocol)

`picklock` includes a dedicated Model Context Protocol (MCP) server that exposes browsing and querying capabilities for all 9 database engines to AI agents (such as Antigravity / agy, Claude Desktop, Cursor, etc.).

### Tools Provided

| Tool | Description |
|---|---|
| `list_objects` | Discover all catalog items (tables, collections, keys, node labels) |
| `browse` | Paginate data with keyset, skip/limit, SCAN, or cursor mechanics |
| `inspect` | Inspect schemas, columns, types, indexes, and engine metadata |
| `query` | Execute native queries (SQL, MQL, RESP, CQL, Cypher). Mutations blocked in read-only mode |
| `connect` | Switch or open database connections dynamically via DSN or saved connection name |
| `list_connections` | Read saved connection profiles from `~/.config/picklock/connections.json` |

### Setup & Configuration

#### 1. Run directly with Go (No install needed)

Run on the fly using `go run`:

```json
{
  "mcpServers": {
    "picklock": {
      "command": "go",
      "args": ["run", "github.com/agmonetti/picklock/cmd/picklock-mcp@latest", "--read-only", "postgres://user:pass@localhost:5432/mydb"]
    }
  }
}
```

#### 2. Install Globally

Install the binary into `$GOPATH/bin`:

```bash
go install github.com/agmonetti/picklock/cmd/picklock-mcp@latest
```

Configuration:

```json
{
  "mcpServers": {
    "picklock": {
      "command": "picklock-mcp",
      "args": ["--read-only", "/path/to/database.db"]
    }
  }
}
```

#### 3. Install from Source

Clone the repository and build the binary locally:

```bash
git clone https://github.com/agmonetti/picklock.git
cd picklock
go build -o picklock-mcp ./cmd/picklock-mcp
```

Configuration:

```json
{
  "mcpServers": {
    "picklock": {
      "command": "/absolute/path/to/picklock/picklock-mcp",
      "args": ["--read-only", "/absolute/path/to/demo.db"]
    }
  }
}
```

> **Note:** The DSN argument is optional. If omitted, the AI agent can use the `connect` tool to connect to any supported database on demand.

## Development

```bash
go run ./cmd/picklock   # run
go test ./...       # tests
go vet ./...        # lint
```

### Integration environment (all 9 engines)

```bash
docker compose up -d --wait

make demo-all       # seeds all 9 engines with sample data
```

## Documentation

- **[USAGE.md](USAGE.md)** — step-by-step guide for relational and non-relational engines.
- **`docs/non-relational/`** — research, composable architecture, engine profiles, interaction design, and technical limitations.
- **`docs/design/`** — original foundational design specifications.

## License

MIT — see [LICENSE](LICENSE).
