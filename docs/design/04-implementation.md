# 04 — Implementation plan

## Order of work

The agent must implement in this exact order. Each phase has verifiable "done" criteria. Do not move to the next phase without completing the previous one.

**Key note for all phases:** the `Store` interface is neutral from day one. The browser, the editor, and the TUI never import a driver. Phases 1-6 build the complete tool with SQLite as the reference engine; phase 7 adds the other four engines. The resulting tool supports exactly five engines: **SQLite, PostgreSQL, MySQL, MariaDB, SQL Server**.

---

## Phase 1 — Scaffold, conn and store (SQLite reference)

**Goal:** the project compiles and `store.New()` returns a `SQLiteStore` that can open a SQLite file.

### Tasks

1. Initialize module: `go mod init github.com/agmonetti/picklock`
2. Add core dependencies:
   ```
   go get github.com/charmbracelet/bubbletea
   go get github.com/charmbracelet/lipgloss
   go get github.com/charmbracelet/bubbles
   go get modernc.org/sqlite
   ```
3. Create the full directory structure (see `02-architecture.md`).
4. Implement `internal/conn/conn.go`:
   - `ConnectionConfig` struct with `Driver`, `Name`, `Path`, `Host`, `Port`, `User`, `Password`, `Database`.
   - `New()` constructor with default driver `sqlite` and default ports per engine.
5. Implement `internal/store/store.go`:
   - `Store` interface (neutral, see `02-architecture.md`).
   - Shared types `Column`, `Result`, `Index`.
6. Implement `internal/store/errors.go`: `ErrUnsupportedDriver`, `ErrConnection`.
7. Implement `internal/store/scan.go`: `ScanResult(rows)` + `Stringify(v)` — convert `database/sql.Rows` to `*Result` (NULL → "", `[]byte`/`time.Time`/numerics → string). Used by all engines.
8. Implement `internal/store/sqlite/`:
   - `store.go`: `New(cfg)` builds the DSN, opens with modernc (driver `"sqlite"`), implements `Tables()`, `Columns()`, `Indexes()`, `Query()`, `Exec()`, `Version()`, `CountTable()`, `SelectTablePage()`, `Close()`.
   - `dialect.go`: `QuoteIdent` (double quotes), `Limit` (`LIMIT n OFFSET m`), introspection via `sqlite_master` + `PRAGMA`.
9. Implement the engine registry in `internal/store/store.go`:
   - `Register(driver, constructor)` + `New(cfg)` that looks it up in the registry.
   - `sqlite` registers itself in its `init()`.
10. Implement `cmd/picklock/main.go`:
    - Launches the TUI (doesn't exist yet — for now it prints `"scaffold ok"` and exits).
    - Imports the engine blank (`_ "picklock/internal/store/sqlite"`) so it gets registered.
    - Smoke tests: open a SQLite file with `store.New` and list tables to stdout (temporary, to verify).

### Done criteria

```bash
$ go build ./cmd/picklock/
$ echo "" | sqlite3 test.db "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"
$ go test ./internal/store/... ./internal/conn/...
# PASS
```

> The temporary `-smoke` flag that printed tables to stdout was removed in
> phase 4 when the TUI replaced it. The real done criterion is `go test`.

---

## Phase 2 — Browser (model, no TUI)

**Goal:** the navigation logic works independently of the TUI and the engine.

### Tasks

1. Implement `internal/browser/browser.go`:
   - `Browser` struct with fields: `Tables`, `ActiveTable`, `Columns`, `Page`, `PageSize`, `TotalRows`, `Rows`, `Cursor`.
   - `New(store Store) (*Browser, error)` — loads tables, selects the first one.
   - `SelectTable(name string, store Store) error`
   - `NextPage(store Store) error` / `PrevPage(store Store) error` — use `store.SelectTablePage` and `store.CountTable`.
   - `Refresh(store Store) error`
   - `MoveCursor(delta int)` — moves cursor within the visible page.
   - `HasNextPage() bool` / `HasPrevPage() bool`
2. Write unit tests in `internal/browser/browser_test.go`:
   - Open an in-memory SQLite (`":memory:"`), create a table, verify that `Browser.Tables` contains it.
   - Verify pagination with 120 rows and pageSize 50.

### Done criteria

```bash
$ go test ./internal/browser/...
# PASS
```

---

## Phase 3 — Editor (model, no TUI)

**Goal:** the editor can run queries and keep history.

### Tasks

1. Implement `internal/editor/history.go`:
   - Ring buffer of 100 strings.
   - `Push(query string)` — adds to history, ignores consecutive duplicates.
   - `Prev() string` / `Next() string` — navigates history.
2. Implement `internal/editor/editor.go`:
   - `Editor` struct with `Buffer`, `History`, `Result`, `Error`, `Mode`.
   - `Execute(store Store) error` — runs `Buffer`, saves to history, saves result or error.
   - `Clear()` — clears the buffer.
   - `returnsRows() bool` — heuristic for whether the query probably returns rows (keywords `SELECT, WITH, PRAGMA, SHOW, EXPLAIN, DESCRIBE, VALUES, TABLE`, case insensitive, trimmed). Not 100% reliable across engines; the store dictates the truth: the UI decides table vs "N rows affected" based on `Result.Columns` and `Result.Affected`, never re-parsing SQL.
3. Unit tests in `internal/editor/editor_test.go` (with in-memory SQLite).

### Done criteria

```bash
$ go test ./internal/editor/...
# PASS
```

---

## Phase 4 — TUI: connection + navigable browser

**Goal:** the tool opens, allows connecting, shows tables and navigating rows.

### Tasks

1. Implement `internal/tui/styles/styles.go` with all the `lipgloss.Style` and adaptive colors.
2. Implement `internal/tui/keys.go`:
   - Define `KeyMap` using `github.com/charmbracelet/bubbles/key`.
   - A `ShortHelp()` and `FullHelp()` for the help panel.
3. Implement `internal/tui/screens/connect.go`:
   - Form with `bubbles/textinput` and engine selector (`bubbles/select` or custom).
   - Changes the visible fields according to the engine.
   - `Enter` emits `connectMsg{cfg}`.
4. Implement `internal/tui/screens/browser.go`:
   - `RenderBrowser(m Model) string` function.
   - Uses `bubbles/table` or renders manually with lipgloss.
   - Columns with auto-calculated width.
   - NULL as `∅` in `ColorNull`.
5. Implement `internal/tui/app.go`:
   - `Model` struct with store, conn, browser, editor, screen, width, height, keys.
   - `Init()` — no store; initial screen `ScreenConnect`.
   - `Update()` — handles `connectMsg` (creates store, loads browser, switches to `ScreenBrowser`), `tea.KeyMsg`, `tea.WindowSizeMsg`.
   - `View()` — renders header + content + footer according to the active screen.
6. Implement `internal/conn/saved.go`:
   - Save/read connections in `~/.config/picklock/connections.json` (chmod 0600).
   - `Ctrl+S` saves, the list is shown in the left connection panel.
7. Update `main.go` to launch `tea.NewProgram(tui.New())`.
8. Implement `internal/tui/screens/structure.go` (minimal functionality: columns; indexes in phase 6).

### Done criteria

```bash
$ go build ./cmd/picklock/
$ echo "" | sqlite3 test.db "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT); CREATE TABLE orders (id INTEGER PRIMARY KEY);"
$ ./picklock
# Should show the connection screen.
# Engine SQLite + path test.db + Enter → browser with sidebar [orders, users].
# Navigate with ↑↓, q to quit.
$ go test ./internal/...
# PASS
```

---

## Phase 5 — SQL editor in TUI

**Goal:** `Tab` switches to the editor, `Ctrl+R` runs, results are visible.

### Tasks

1. Implement `internal/tui/screens/editor.go`:
   - `RenderEditor(m Model) string` function.
   - Input with `bubbles/textarea`.
   - Results below with the same table as the browser.
   - Errors in red.
   - "STATUS: OK (N rows affected, duration)" for INSERT/UPDATE/DELETE (based on `Result`).
2. Add `Tab` handling in `Update()` to toggle `ScreenBrowser ↔ ScreenEditor`.
3. Integrate history: `↑`/`↓` when the textarea is on the first/last line navigates history.

### Done criteria

```bash
$ ./picklock
# Connect to test.db → Tab opens the editor.
# Type "SELECT * FROM users;" and Ctrl+R shows the results.
# Type an invalid query and Ctrl+R shows the error in red without crashing.
# ↑ navigates query history.
```

---

## Phase 6 — Table structure + polish

**Goal:** `i` shows the full structure, everything looks good, ready for real use.

### Tasks

1. Complete the structure screen:
   - Columns with name, type, constraints (PK, NOT NULL, DEFAULT).
   - Indexes with name, columns, UNIQUE.
2. Loading spinner for slow queries (>100ms).
3. Empty states:
   - Empty table → `empty table`
   - Database with no tables → `no tables — use the editor to create one`
4. `Ctrl+N` new connection: closes store, resets browser/editor, returns to `ScreenConnect`.
5. Correct resizing: `tea.WindowSizeMsg` must re-render everything without artifacts.
6. README.md with: what it is, installation (`go install`), basic usage, all shortcuts.

### Done criteria

- Connect to a real DB with 10k+ rows and navigate without perceptible lag.
- Resize the terminal and the layout must not break.
- `go vet ./...` without warnings.
- `go build ./...` without warnings.

---

## Phase 7 — The other four engines

**Goal:** `picklock` supports PostgreSQL, MySQL, MariaDB and SQL Server with the SAME UI. Nothing outside `internal/store/**` changes.

### Tasks

1. Implement `internal/store/postgres/`:
   - Driver: `jackc/pgx/v5` (wrapping `database/sql` via `pgx/stdlib`).
   - `dialect.go`: `"ident"`, `LIMIT n OFFSET m`, introspection with `information_schema` + `pg_indexes`.
   - `Version()` via `SELECT version()`.
2. Implement `internal/store/mysql/`:
   - Driver: `go-sql-driver/mysql`. A single package for MySQL and MariaDB.
   - `NewMySQL(cfg)` and `NewMariaDB(cfg)`.
   - `dialect.go`: `` `ident` ``, `LIMIT n OFFSET m`, introspection with `information_schema`.
   - `Version()` via `SELECT version()` (MariaDB replies with its own string).
3. Implement `internal/store/mssql/`:
   - Driver: `microsoft/go-mssqldb`.
   - `dialect.go`: `[ident]`, `OFFSET m ROWS FETCH NEXT n ROWS ONLY`, introspection with `INFORMATION_SCHEMA` + `sys.indexes`.
   - `Version()` via `SELECT @@VERSION`.
4. Register the five drivers in the `store` registry (each engine in its `init()`) and import them blank from `cmd/picklock/main.go`.
5. Per-engine integration tests (see `05-technical-decisions.md`): triggered by env vars, with real DBs in docker. The repo's `compose.yaml` starts the 4 engines with fixed credentials and the auto-created `test` database:
   ```bash
   docker compose up -d
   docker compose ps   # all 4 must be healthy
   ```
   Tests skip (`t.Skip`) if the env var is not set. Env vars: `PICKLOCK_TEST_POSTGRES_HOST` / `_USER` / `_PASSWORD` / `_DATABASE` (same for `MYSQL`, `MARIADB`, `MSSQL`).

### Done criteria

```bash
$ go test ./internal/store/...   # includes integration if env vars are set
$ go test ./internal/browser/... ./internal/editor/... ./internal/conn/...
# PASS — without changing ANYTHING in the browser/editor/TUI between engines.
$ ./picklock
# Connect to each of the 4 engines in docker and verify:
#   - sidebar with correct tables
#   - row navigation with pagination
#   - SELECT / INSERT / UPDATE / DELETE from the editor
#   - i shows correct columns and indexes
```

---

## Phase 8 — Release

**Goal:** distributable binary.

### Tasks

1. Add `Makefile` with targets: `build`, `test`, `lint`, `clean`, `demo` (creates a sample SQLite database), `release` (multi-platform).
2. Build for multiple platforms:
   - `linux/amd64`, `darwin/amd64`, `darwin/arm64` (with CGO for SQLite or documenting `modernc.org/sqlite`).
3. Verify binary size (< ~40 MB) and document in README.
4. Tag `v0.1.0` in git.

---

## Constraints for the agent

- **Do not use `panic()`** anywhere except in `main()` for fatal initialization errors.
- **Do not use `fmt.Println`** inside the `internal/` packages. All messages go through the bubbletea message system.
- **Do not introduce new dependencies** without explicit justification in a comment.
- **Do not modify the `Store` interface** without updating this document and `02-architecture.md`.
- **Do not add engines** outside SQLite, PostgreSQL, MySQL, MariaDB and SQL Server.
- **No layer outside `internal/store/**` may import `database/sql`** nor a driver.
- If a task in a phase is ambiguous, implement the simplest option that passes the done criteria. No overengineering.
