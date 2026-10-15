# 05 — Technical decisions, edge cases and conventions

## Code conventions

### Naming

- Packages in lowercase, one word: `conn`, `store`, `browser`, `editor`, `tui`.
- Exported structs with descriptive names: `SQLiteStore`, `PGStore`, `MySQLStore`, `MSSQLStore`, `Browser`, `Editor`.
- Interfaces suffixed by behavior, not implementation: `Store` (not `IStore`, not `StoreInterface`).
- Errors prefixed with `Err`: `ErrUnsupportedDriver`, `ErrConnection`, `ErrTableNotFound`, `ErrInvalidQuery`.
- Screen constants as their own type: `type Screen int` with `const (ScreenConnect Screen = iota; ScreenWorkspace)`. Pane focus is `screens.WorkspaceFocus` (`FocusSidebar`, `FocusMain`, `FocusEditor`).
- Engine constructors by driver name: `sqlite.New`, `postgres.New`, `mysql.NewMySQL`, `mysql.NewMariaDB`, `mssql.New`.

### Function structure

- Prefer functions under 40 lines.
- If a function needs more, extract subfunctions with descriptive names.
- Early return for errors: don't nest `if err != nil` in a cascade.

```go
// Good
func (s *SQLiteStore) Tables() ([]string, error) {
    rows, err := s.db.Query("SELECT name FROM sqlite_master WHERE type='table' ORDER BY name")
    if err != nil {
        return nil, fmt.Errorf("store.Tables: %w", err)
    }
    defer rows.Close()
    // ...
}

// Bad — don't do this
func (s *SQLiteStore) Tables() ([]string, error) {
    if rows, err := s.db.Query("..."); err == nil {
        // nested logic
    } else {
        return nil, err
    }
}
```

- Always wrap errors with `fmt.Errorf("context: %w", err)` so they are traceable.

### Comments

- Comment the "why", not the "what".
- Every exported function has a godoc comment.
- Complex SQL queries have a comment explaining what they return.

### Per-engine dialects

| Engine | QuoteIdent | Pagination |
|---|---|---|
| SQLite | `"name"` | keyset `ORDER BY "pk"` / `LIMIT n` (OFFSET fallback) |
| PostgreSQL | `"name"` | keyset `ORDER BY "pk"` / `LIMIT n` (OFFSET fallback) |
| MySQL / MariaDB | `` `name` `` | keyset `ORDER BY "pk"` / `LIMIT n` (OFFSET fallback) |
| SQL Server | `[name]` | keyset `ORDER BY [pk]` / `OFFSET..FETCH` (OFFSET fallback) |

### Per-engine schema metadata

| Engine | Tables / columns / indexes | Foreign-key metadata |
|---|---|---|
| SQLite | `sqlite_master` + `PRAGMA` | `PRAGMA foreign_key_list` |
| PostgreSQL | `information_schema` + `pg_indexes` | `information_schema.key_column_usage` + `referential_constraints` |
| MySQL / MariaDB | `information_schema` | `KEY_COLUMN_USAGE` + `REFERENTIAL_CONSTRAINTS` |
| SQL Server | `INFORMATION_SCHEMA` + `sys.indexes` | `sys.foreign_keys` + `sys.foreign_key_columns` |

Foreign keys are exposed in relational inspection with their ordered source and
referenced columns, update rule, and delete rule. The catalog keeps every
physical table browseable, but groups a table under `RELATIONS` only when it
has at least two foreign keys and every column participates in one of them.
Names alone never imply a relationship; schemas without declared constraints
are classified conservatively as ordinary tables.

### Per-engine pagination

- Tables with a single-column primary key are browsed with **keyset pagination**
  (`WHERE "pk" > <last key> ORDER BY "pk" LIMIT n`), so refreshing never moves
  the visible rows and pages do not skip/duplicate rows between refreshes. The
  browser fetches one extra row to know whether another page exists.
- PostgreSQL casts the bound parameter (`$1::<data_type>` with the type read
  from `information_schema` and cached per table+column) so the primary-key
  index stays usable regardless of the column type.
- Tables with composite primary keys use OFFSET pagination ordered by the full
  primary-key tuple. Without a primary key, a unique, non-partial index whose
  columns are all `NOT NULL` is used for stable ordering when available.
  Partial/filtered and expression indexes are not treated as global keys. Other
  tables fall back to `ORDER BY 1`; concurrent writes can reorder pages.
- Relational browse requests one lookahead row instead of running `COUNT(*)`
  for every page. Totals are unknown (`-1`); paging uses that extra row.
- `LIMIT n` uses integers (page, pageSize), safe for direct interpolation.
- Identifiers (table/column names) are ALWAYS escaped with `QuoteIdent`. Generated
  Browse SQL selects explicit projected expressions; it no longer uses `SELECT *`.
- `Result.Rows` is `[][]string` in all engines. Native values are serialized for
  display; `Result.Nulls [][]bool` remains authoritative for SQL NULL versus an
  empty string. `Incomplete` marks a preview; `RowRefs` hold typed hidden keys.
  Manually-built results may leave these metadata fields nil.

### Per-engine Browse projection

The relational adapter applies the same column/filter validation and binds filter
values as parameters. Each SQL driver builds only the requested visible columns,
plus native identity fields required for navigation. A key used as a visible
direct scalar is reused for its `RowReference`; projected/truncated values are
never used to reconstruct an identity.

| Engine | Metadata classification and server expression | Bound and representation |
|---|---|---|
| SQLite | Classifies each stored value with `typeof`, regardless of declared affinity. Text uses `substr` by characters; BLOB uses byte substring; other dynamic values pass through. | The driver returns at most `preview_bytes` characters/bytes at SQL level; Go retains no more than that many bytes and removes an incomplete UTF-8 suffix. Runtime type (`TEXT`/`BLOB`) and NULL are separate flags. Binary previews render as `0x` hex. |
| MySQL / MariaDB | Character/text/enum/set/JSON types are cast to `utf8mb4` then byte-substringed; blob values are byte-substringed. Spatial values use bounded WKB. Known scalar types stay native; unknown types fail the browse rather than falling back to full values. | `OCTET_LENGTH` sets preview metadata. Text is capped at `preview_bytes` and made valid UTF-8; binary previews render as `0x` hex. MariaDB is independently exercised despite sharing the driver. |
| PostgreSQL | Known scalar types stay native. Character, `name`, JSON/JSONB/XML and other types (including domains/enums/arrays/extensions) use a `::text` prefix; `bytea` uses byte substring. | Text is capped to `preview_bytes` in UTF-8 bytes after the SQL character prefix; `octet_length(convert_to(..., 'UTF8'))` marks incompleteness. Binary previews render as `0x` hex. |
| SQL Server | Character types use `SUBSTRING`; Unicode, XML, `sql_variant`, spatial and `hierarchyid` types are converted to a bounded textual representation; binary/image/rowversion use byte substring. Known scalar types stay native; unrecognized base types fail safely. | `DATALENGTH` flags source truncation; Go additionally caps returned text by UTF-8 byte count and trims to a valid boundary. Binary previews render as `0x` hex. |

`preview_bytes` limits retained textual preview bytes, not total process RSS.
Expressions bound the result returned for each selected cell, but SQL Server,
SQLite and PostgreSQL may internally read/convert a larger value to produce a
prefix. No protocol-level network-byte measurement is available from these
drivers; integration evidence measures returned `Browse` values, not physical
wire traffic. `SELECT` statements issued by the user remain unchanged.

## Export to CSV/JSON

- Triggered with `Alt+E` from the workspace. Data source: the last editor query
  result when the editor is focused, the current page of the active table
  otherwise (a note in the success message states the page when the table has
  more rows).
- A centered prompt (bubbles `textinput`) takes the target file name, pre-filled
  with `picklock-export-<timestamp>.csv`. The format follows the extension
  (`.json` → JSON; anything else → CSV). `Enter` writes the file synchronously
  (≤10k rows) and shows `exported N rows → /abs/path`; `Esc` cancels. A write
  error keeps the prompt open so the user can fix the name.
- `internal/export` serializes a `store.Result` that is already in memory; it
  never touches the database or a driver. CSV is RFC 4180 (`encoding/csv`): NULL
  and empty string both become empty fields. JSON is an array of objects in
  column order; NULL becomes `null` (via `Result.Nulls`), everything else stays
  a string (picklock's model is strings-only). HTML escaping is disabled so values
  like `a < b` are exported verbatim.
- Full-table export (beyond the current page) needs a streaming `Store` method
  and is out of scope until then.

## Connection and DSN per engine

### SQLite (modernc.org/sqlite)

Pure-Go driver (no CGO/gcc). It registers the `"sqlite"` driver; pragmas go
through `_pragma=` query parameters:

```go
// Read/write mode (default)
dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)", path)

// Read-only mode ("Read only" toggle in the form, cfg.ReadOnly)
dsn := fmt.Sprintf("file:%s?mode=ro&_pragma=foreign_keys(1)", path)
```

- `_pragma=journal_mode(WAL)`: better performance for concurrent reads. Not applicable in read-only.
- `_pragma=foreign_keys(1)`: SQLite doesn't enable them by default. Always enable.
- `_pragma=busy_timeout(5000)`: wait up to 5 seconds before returning `SQLITE_BUSY`.
- `ReadOnly` is also persisted in saved connections (`connections.json`, field `read_only`).
- The pure-Go driver removes the gcc requirement and enables cross-compilation with `CGO_ENABLED=0`.

### PostgreSQL (jackc/pgx/v5)

```go
// DSN via net/url in the code. sslmode comes from cfg.SSLMode (default "prefer").
u := url.URL{
    Scheme: "postgres",
    User:   url.UserPassword(cfg.User, cfg.Password),
    Host:   fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
    Path:   "/" + url.PathEscape(cfg.Database),
}
q := u.Query()
q.Set("sslmode", cfg.SSLMode) // or "prefer" if empty
u.RawQuery = q.Encode()
```

- Use `pgx/v5/stdlib` (wrapping `database/sql`). It's the most maintained integration.
- `sslmode` configurable from the form (`SSL` field) and persisted in saved connections. Default `prefer`; the TLS error is shown verbatim if it fails.
- Connection timeout of 5s: wrap `sql.Open` + `Ping` in `context.WithTimeout`.

### MySQL and MariaDB (go-sql-driver/mysql)

```go
dsn := fmt.Sprintf(
    "%s:%s@tcp(%s:%d)/%s?parseTime=true&charset=utf8mb4&timeout=5s",
    cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.Database,
)
```

- The same package works for both. MariaDB is registered separately because `Version()` and some `information_schema` responses differ.
- `parseTime=true` so `time.Time` doesn't arrive as `[]byte`.
- `timeout=5s` to avoid hanging on unreachable hosts.
- Don't use `multiStatements=true` (security).
- TLS via `mc.TLSConfig`: `"preferred"` (prefer), `"true"` (require, verifies the server hostname+CA) or `"false"` (disable). Empty keeps the driver default (no TLS unless the server asks).

### SQL Server (microsoft/go-mssqldb)

```go
dsn := fmt.Sprintf(
    "sqlserver://%s:%s@%s:%d?database=%s&connection+timeout=5",
    url.QueryEscape(cfg.User), url.QueryEscape(cfg.Password),
    cfg.Host, cfg.Port, url.QueryEscape(cfg.Database),
)
```

- `connection+timeout=5` seconds.
- Identifiers are escaped with `[...]`.
- TLS via the URL params: `encrypt=mandatory` (require) or `encrypt=disable`.
  `prefer` (and the default) keep the driver default, which encrypts when the
  server supports it **without demanding a trusted certificate** — so local
  dev servers with self-signed certificates still connect, which the compose
  container is one of.

## TLS per engine

The `SSL` field of the form (all network engines) and `?sslmode=`/`?tls=` in a
DSN feed `cfg.SSLMode`, persisted in `ssl_mode`:

| Engine | prefer | require | disable | extra |
|---|---|---|---|---|
| PostgreSQL | `sslmode=prefer` | `sslmode=require` | `sslmode=disable` | `verify-ca`, `verify-full` |
| MySQL / MariaDB | `mc.TLSConfig="preferred"` | `mc.TLSConfig="true"` | `mc.TLSConfig="false"` | — |
| SQL Server | driver default (encrypts without cert validation) | `encrypt=mandatory` | `encrypt=disable` | — |

The form validates the accepted values per engine, and a connection against a
server with an untrusted certificate fails with the driver's literal error
(shown in the footer), so the user knows TLS did not verify.

## Edge cases to handle

### Connection

| Case | Expected behavior |
|---|---|
| Unreachable host / connection refused | Error in footer: `cannot connect to localhost:5432: connection refused`. Return to `ScreenConnect` without crashing. |
| Wrong credentials | Engine's literal text: `password authentication failed for user "postgres"`. |
| Database doesn't exist | Engine's literal text (varies: `database "x" does not exist`, `Unknown database 'x'`). |
| Connection timeout (>5s) | `timeout connecting to localhost:5432`. |
| Unknown driver | `ErrUnsupportedDriver` (shouldn't happen: the UI selector only offers the 5). |
| SQLite: file doesn't exist | Error on the connection screen: `"archivo.db: no such file"`. |
| SQLite: file is not SQLite | `"not a valid SQLite database"`. |
| SQLite: file in exclusive use | SQLite returns `SQLITE_BUSY`. Show: `"the database is locked by another process"`. |
| SQLite: corrupted database | Show SQLite's literal error. |
| SQLite: empty file (0 bytes) | SQLite accepts it as a valid DB with no tables. Normal empty state. |

### Tables and data

| Case | Expected behavior |
|---|---|
| Table with no rows | Show column headers, empty area, text `empty table`. |
| Column with NULL value | Show `∅` in gray. |
| Text exceeding its column width (at most 36 terminal cells) | Truncate with `…` inside that budget, preserving grapheme clusters. |
| Very long column name | Truncate too, with `…`. |
| Table name with spaces or special characters | Escape with the dialect's `QuoteIdent`: `"my table"`, `` `my table` ``, `[my table]`. |
| Database with 500+ tables | The sidebar shows a scrollable subset. Don't load everything into render memory. |
| Query returning 0 columns (e.g. `PRAGMA ...`) | Show the raw result as text, not as a table. |
| Binary type (`bytea`, `BLOB`, `varbinary`) | Convert to a string representation in the store: `0x...` or short base64, truncated in the UI. |
| `time.Time` / `decimal` types | The store serializes them to string. The UI never does type assertions. |

### SQL editor

| Case | Expected behavior |
|---|---|
| Empty query + `Ctrl+R` | Don't execute anything. Message: `write a query first`. |
| Query ending with semicolon | Works normally. |
| Multiple statements separated by `;` | Execute the statement under the cursor (by line). If several statements are on the same line, the first one is chosen. The split respects strings (`'...'`, `''`, `\`). |
| `DROP TABLE users` | Execute normally. `picklock` doesn't ask for confirmation (the user knows what they're doing). |
| Query taking >5 seconds | No execution timeout by default. Show a spinner. The query runs with `context.WithTimeout` using the user's configured timeout (`Ctrl+P`, default 60s); `Esc` cancels it (both abort the driver call via `QueryContext`/`ExecContext`). |
| Unsupported dialect | The engine's literal SQL error is shown in red. The user writes SQL for the engine they're connected to. |

### Terminal

| Case | Expected behavior |
|---|---|
| Very small terminal (<60 cols) | Hide the sidebar automatically. If <40 cols, show a warning: `terminal too small`. |
| Very small terminal (<10 rows) | Show only header + footer with an error. |
| Resize during query | The resize is processed when the query finishes. No concurrency. |
| Mouse | Enabled with cell motion (drag-only reporting). A left click focuses a pane; a right-click drag resizes the nearest divider (sidebar width or editor height), persisted in `prefs.json`. Only active on the workspace screen. |
| Terminal without color support | lipgloss detects it automatically. Render without colors if `TERM=dumb` or similar. |
| `SIGTERM` / `Ctrl+C` | Exit cleanly: `store.Close()` before terminating. |

## Read-only mode

Available for all five engines via the `Read-only` toggle in the form, the
persisted `read_only` field of a saved connection, or the global `--read-only`
flag (merged as `cfg.ReadOnly = cfg.ReadOnly || globalReadOnly` before opening).

| Engine | Enforcement |
|---|---|
| SQLite | DSN `mode=ro` (no WAL). Writes fail with `attempt to write a readonly database`. |
| PostgreSQL | `options=-cdefault_transaction_read_only=on` in the DSN. Every transaction starts read-only. |
| MySQL / MariaDB | No DSN parameter exists. `Exec`/`ExecContext` pin a connection (`sql.DB.Conn`), run `SET SESSION TRANSACTION READ ONLY` on it and execute the statement there, so a write fails at the server. Reads still use the normal pool. |
| SQL Server | No per-session read-only toggle exists in T-SQL. Not enforced; the TUI shows an advisory warning and the docs recommend connecting with a read-only user. |

## Saved connections

- File: `~/.config/picklock/connections.json`, permissions `0600`.
- Structure:
  ```json
  [
    { "name": "local", "driver": "postgres", "host": "localhost", "port": 5432,
      "user": "postgres", "password": "", "database": "mydb", "path": "",
      "ssl_mode": "prefer" },
    { "name": "prod", "driver": "sqlite", "path": "/data/app.db", "read_only": true }
  ]
  ```
- `password` is optional ("save password" field in the form). If saved, it goes as plain text in the JSON — tradeoff documented in `06-security.md`. The OS keychain remains a future improvement.
- `read_only` (SQLite) and `ssl_mode` (PostgreSQL) are also persisted and reused when connecting.
- The password is never shown in the form (masked field). If the saved connection has no password, the field is left empty for the user to enter it.
- The file is read when opening `ScreenConnect` and written with `Ctrl+S`. File read/write errors are silently ignored (they don't break the session).

## Preferences

- File: `~/.config/picklock/prefs.json`, permissions `0600`.
- Structure:
  ```json
  {
    "query_timeout_seconds": 60,
    "browse_page_size": 50,
    "preview_bytes": 256,
    "value_block_bytes": 65536,
    "editor_max_cell_bytes": 65536,
    "editor_max_result_bytes": 16777216,
    "editor_max_rows": 10000,
    "sidebar_width": 0,
    "editor_height": 0
  }
  ```
- Query timeout bounds editor and browser operations (`context.WithTimeout`),
  from 1 to 86400 seconds. All row/byte limits are positive, integer overflow
  is rejected, preview bytes ≤ block bytes and editor cell bytes ≤ result bytes.
- Old files inherit missing defaults. Invalid files return an error plus safe
  defaults; invalid saves do not overwrite the previous preferences.
- Browse page size is the default for new table openings/connections; active
  selection changes use `Browser.SetOptions`, resetting cursor history.
- `preview_bytes` is enforced by relational Browse projections; full-value
  blocks remain E5 and editor byte retention remains E6. Filters/options/data
  are never persisted in `Prefs`.
- `sidebar_width` / `editor_height` persist the workspace pane sizes changed
  with a right-click drag; `0` means automatic. The values are clamped again
  against the current terminal size on every render.
- Edited from the settings screen (`Ctrl+P`, available from the connect and
  workspace screens; `Tab`/`Shift+Tab`/`↑↓` select fields) and saved with `Enter`.
  Timeout/editor rows apply to the next query; page defaults to a new opening.
- Same directory resolution as `connections.json` (`PICKLOCK_CONFIG_DIR` override).

## User-facing messages

All visible messages follow these rules:

- In English (the project was Spanish-first by philosophy; it can be internationalized later).
- No unnecessary capitalization. Lowercase at the start except for proper nouns.
- No exclamation marks.
- No "error:" prefix — the red color already communicates the error.
- Concise: at most two lines in the footer.

Examples:

```
# Good
empty table
cannot connect to localhost:5432: connection refused
invalid query: near "FORM": syntax error
nothing to export — run a query that returns rows
exported 50 of 5000 rows (current page) → /tmp/out.csv

# Bad
Error! The query could not be executed because the syntax is incorrect.
ERROR: PERMISSION DENIED
```

## Testing

- Tests in `_test.go` in the same package (white box testing).
- **SQLite:** use `":memory:"` as path for store, browser and editor tests — creates no files and is the reference engine for all UI tests.
- **Other engines:** integration tests inside `internal/store/<engine>/`, triggered by separate env vars. If the var isn't set, `t.Skip`:
  ```go
  // PICKLOCK_TEST_POSTGRES_HOST=localhost
  // PICKLOCK_TEST_POSTGRES_USER=postgres
  // PICKLOCK_TEST_POSTGRES_PASSWORD=postgres
  // PICKLOCK_TEST_POSTGRES_DATABASE=test
  // (same pattern with MYSQL, MARIADB, MSSQL)
  ```
  - The setup creates a test table `picklock_test`, runs the same assertions as the SQLite test.
  - Docker for local development: the repo's `compose.yaml` starts the 4 engines with fixed credentials and the auto-created `test` database (`docker compose up -d`). Full commands in `README.md`.
- Don't mock `Store` for browser/editor tests — use the real implementation with in-memory SQLite. It's more reliable.
- Test names: `TestBrowser_SelectItem_LoadsColumns`, `TestStore_Query_ReturnsError_OnInvalidSQL`.
- No external testing frameworks. Only stdlib `testing` and `errors.Is` for assertions.

## Performance considerations

- Relational browsing no longer runs `COUNT(*)` for each page. It requests one
  extra row and uses it only as a lookahead; totals remain unknown (`-1`).
  Exact count is still available through the explicit table-count API.
- Structured column selection and typed filters are applied in the server query
  by the four relational driver implementations (MySQL also covers MariaDB).
  Default Browse uses explicit projections and bounded previews for potentially
  large text/JSON/binary values; native `RowReferences` keep navigation keys
  hidden when not selected. The options form is E4; complete-value reading is E5.
- Browse results retain only the current page; `Browser.Clone` shares its
  immutable page, and a single cancellable navigation replaces it on success.
  Totals remain unknown and lookahead controls the next-page affordance.
- Browser navigation (open table, change page, refresh) runs in the background,
  like editor queries: it is bounded by the configured query timeout, cancellable
  with `Esc`, and shows a spinner instead of freezing the UI.
- Editor executors receive `store.QueryLimits`; `editor_max_rows` configures
  the existing row cap (default 10000), while cell/result byte enforcement
  remains E6. Marked partial/cutoff results are visible and cannot be copied
  or exported as complete. Legacy row cutoffs do not invent retained-byte counts.
- Column widths are recomputed during rendering from at most 100 supplied rows.
  Each measurement stops at 36 terminal cells; once a column reaches that cap,
  later sampled cells in that column are skipped. There is no width cache.
- Cell truncation walks the prefix through the visual budget plus one grapheme
  of lookahead, rather than repeatedly measuring/copying the whole value.
  It uses the existing `uax29` grapheme iterator and `runewidth` width policy;
  the ellipsis is part of the budget and callers apply ANSI styling afterwards.
- Row detail wrapping stops at the viewport end and uses string slices rather
  than repeated rune concatenation. Field navigation/hit testing still counts
  full wrapped fields, and deep scroll scans the preceding prefix. Raw text
  still searches for newlines, and graph/document formatting still has metadata
  overhead; these are not general constant-time rendering guarantees.
- Grid rendering bounds hidden-text work independently of relational Browse,
  which now caps returned cell projections. SQL engines may still read/convert
  larger values internally; the driver protocol does not expose transferred-byte
  counts here. A single unusually long grapheme/zero-width sequence can itself
  require substantial scanning. Full-value reading is still E5.
- `View()` in bubbletea is called every frame. Don't do I/O there. Only string building.
- `lipgloss.Render()` is expensive in loops. Precompute styles as package-level variables, don't create them on every `View()` call.
- The `[]byte` → string conversion of each cell happens only once in the store, when reading the row. Don't repeat it in the UI.

## Expected initial go.mod

```
module github.com/agmonetti/picklock

go 1.22

require (
    github.com/charmbracelet/bubbles v0.20.0
    github.com/charmbracelet/bubbletea v1.2.0
    github.com/charmbracelet/lipgloss v1.0.0
    github.com/go-sql-driver/mysql v1.8.1
    github.com/jackc/pgx/v5 v5.7.1
    github.com/microsoft/go-mssqldb v1.7.2
    modernc.org/sqlite v1.56.0
)
```

Exact versions to verify with `go get` at implementation time — use the latest stable ones.

## What comes after v0.1.0 (out of scope for now)

So the agent knows what NOT to implement yet:

- New engines (Oracle, DB2, Snowflake, NoSQL). The list is closed at five.
- Inline cell editing (too complex for v0.1).
- SQL syntax highlighting in the editor.
- Saving passwords to the OS keychain.
- Plugin system.

If any of these features seems necessary to get something basic working, check before implementing.

## Persistent query history

- The in-memory ring buffer (100 entries) is mirrored to
  `~/.config/picklock/history.json` (0600, `PICKLOCK_CONFIG_DIR` override like the
  other files). `editor.SaveHistory` writes the whole list after each query;
  the ring already caps and dedupes, so the file is always the truth of the
  in-memory history. `editor.LoadHistory` seeds the editor on startup and on
  every new session.
- Written from the UI goroutine only (inside `editorDoneMsg`, next to
  `History.Push`), never from a query goroutine, so there is no file race.
  Read/write errors are ignored: a missing or corrupt history just starts empty.
- **Plaintext by design:** a query can carry inline credentials (e.g.
  `SET ROLE`/`USE` with a password), and nothing is filtered. The file is 0600
  and that risk is documented in `06-security.md`; the user controls what they
  run and store.

## DSN shortcut (`picklock <dsn>`)

- `conn.ParseDSN` converts one command-line argument into a `ConnectionConfig`:
  a bare path (no scheme) is a SQLite file; `sqlite:`/`file:` URIs work too for
  absolute paths; `postgres://`, `mysql://`, `mariadb://` and `sqlserver://`
  parse user/password/host/port/database with the engine default port and
  `localhost` as fallbacks. Anything else is an error reported before the TUI
  starts (`picklock: ...` on stderr, exit 1).
- The database comes from the URL path (`postgres://h/dbname`), except SQL Server
  where the standard `?database=` query parameter is used (its URL format has no
  path database).
- `?sslmode=` or `?tls=` in the query maps to `ConnectionConfig.SSLMode`, so TLS
  can be selected per invocation without the form.
- `main.go` puts the parsed config in `tui.NewOpts.InitialCfg`; `Model.Init()`
  runs `doConnect` immediately, landing on the workspace with the connecting
  spinner. A failed initial connection falls back to the connection screen with
  the error shown, so `Ctrl+N`/the form remain the escape hatch.
