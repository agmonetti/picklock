# LESSONS — agent lessons

This file documents crossroads and decisions the agent makes during development, so they can be reused in the future. It is updated continuously.

---

## L-01 — Go module without a real repo: don't use the literal `[usuario]`

**Crossroads:** the SPEC says `go mod init github.com/[usuario]/picklock`, but `[usuario]` with brackets is not a valid module path for Go and there is no real repo.

**Decision:** use `module picklock` (simple name). When the project was published to `github.com/agmonetti/picklock`, it was changed to `module github.com/agmonetti/picklock` with a `sed` (`s|"picklock/|"github.com/agmonetti/picklock/|g` over the `*.go` files plus the `module` line in `go.mod`), which enables `go install github.com/agmonetti/picklock@latest`.

**Lesson:** when the SPEC has invalid placeholders, resolve with the simplest option and note it here instead of asking. The module path is migrated on publish with a mechanical sed.

---

## L-02 — Minimal stack first, features later

**Crossroads:** the SPEC asks for `cobra` as optional, spinners, etc. Starting everything at once complicates debugging.

**Decision:** implement in SPEC phases. Phase 1 = only `store` + `conn` + `cmd` with output to stdout. No TUI yet.

**Lesson:** follow `design/04-implementation.md` to the letter. Each phase has a done criterion verifiable with `go test`/`go build`. Don't skip phases.

---

## L-03 — The `Store` interface is the sacred contract

**Crossroads:** the TUI needs to know if a query returns rows or "rows affected" to render.

**Decision:** `Query()` always returns `*Result` with `Columns`/`Rows`. The UI decides how to display based on `len(Result.Columns)`. `Exec()` is for `rowsAffected`. Never import `database/sql` outside `internal/store/**`.

**Lesson:** keeping `Store` neutral is what makes it possible to add engines without touching the UI. If a requirement seems to demand engine logic outside, that's a sign the interface is wrong — fix the interface.

---

## L-04 — Dependencies with CGO complicate the build, have a plan B

> **Superseded by L-23:** SQLite now uses `modernc.org/sqlite` (pure Go). Kept as history.

**Crossroads:** `mattn/go-sqlite3` requires gcc. There is gcc on this machine, but cross-compiling will fail.

**Decision:** use `mattn` for now (the SPEC asks for it). Leave the `modernc.org/sqlite` alternative documented for CGO-less builds.

**Lesson:** the cost of CGO is paid at release time. Note the escape hatch in the README and in `design/05-technical-decisions.md`.

---

## L-05 — Engine per package + registration in `init()` avoids the import cycle

**Crossroads:** `store` (factory) needs to import `sqlite`, but `sqlite` imports `store` for the shared types (`store.Store`, `store.Column`...) → import cycle. It's inevitable with the SPEC's pattern.

**Decision:** registry pattern:
- `store` defines the interface, types, errors and a `Register(driver, constructor)` + `New(cfg)` that looks up the registry.
- Each engine calls `store.Register(...)` in its `init()`.
- `cmd` imports the engines blank (`_ "picklock/internal/store/sqlite"`) so they register.
- `store` does NOT import any engine → no cycle.

**Lesson:** the "plugin registry with init()" is the Go pattern to avoid cycles between a contract and its implementations. The registry is build-time configuration, not runtime state — it doesn't violate "no global state".

---

## L-06 — SQLite creates nonexistent files: validate with os.Stat

**Crossroads:** the SPEC requires "file doesn't exist → error", but the SQLite driver creates a blank file when opening a nonexistent path.

**Decision:** `os.Stat(cfg.Path)` before opening; if it doesn't exist, `ErrConnection: no such file`. Exception: `:memory:` and `file::memory:` (in-memory databases for tests) skip the check.

**Lesson:** SQLite drivers have auto-creation behaviors that contradict the expected contract. Validate at the store layer, don't assume.

---

## L-07 — Factory tests go in an external package (`package store_test`)

**Crossroads:** the `store` factory test needs to import `sqlite` (to register it), and `sqlite` imports `store` → import cycle in the test.

**Decision:** test in `package store_test` (external test). The external package can import packages that import the package under test, without creating a cycle.

**Lesson:** when the registry pattern creates cycles, contract tests go in `package <name>_test`. If something "is unused" in the test after the change, it's a sign the calls were left without a prefix (`New` → `store.New`).

---

## L-08 — `Result` needs to distinguish a table from "rows affected"

**Crossroads:** the UI decides how to show a result: table if there are columns, "N rows affected" for INSERT/UPDATE/DELETE. The `Store` interface had no way to express the latter.

**Decision:** add `Affected int64` to `store.Result`. Read queries set it to `-1`; `Exec` sets it to `rowsAffected`. The UI decides with `len(Columns) > 0` (table) or `Affected >= 0` (rows affected).

**Lesson:** when a UI requirement has nowhere to live in the model, the model is incomplete — don't patch the UI. A new field on `Result` is simpler than inspecting strings in the TUI.

---

## L-09 — The SELECT heuristic is fragile; let the store dictate the truth

**Crossroads:** `Query()` or `Exec()` for the editor buffer? `strings.HasPrefix(SELECT)` fails with `WITH ... SELECT`, `PRAGMA`, `EXPLAIN`.

**Decision:** the editor uses a short list of keywords that return rows (`SELECT, WITH, PRAGMA, SHOW, EXPLAIN, DESCRIBE, VALUES, TABLE`) and documents it as a hint. The final result is dictated by the store (a `Result` with columns vs `Affected`), not the editor.

**Lesson:** every presentation decision is based on the store's `Result`, never on re-parsing SQL at the UI layer.

---

## L-10 — `Ctrl+I` and `Tab` are indistinguishable in the terminal

**Crossroads:** the SPEC mapped `Ctrl+I` to "view structure". When testing the TUI, `Ctrl+I` opened the editor: the `Tab` handler captured it.

**Decision:** in the terminal `Ctrl+I` and `Tab` are the same byte (0x09, HT). bubbletea normalizes them to `KeyTab` (its `String()` is "tab"), so a `ctrl+i` binding never matches. I changed structure inspection to `i` (info) and updated the docs.

**Lesson:** never design keymaps with `Ctrl+I` if `Tab` is taken. Before writing a binding, verify it doesn't collide with another control key with the same ANSI code (`Ctrl+J`=`Enter`, `Ctrl+M`=`Enter`/CR, `Ctrl+H`=`Backspace`, `Ctrl+I`=`Tab`). This is a class of bug that only appears when testing the real TUI, not in model unit tests — which is why it's worth testing the full flow with keys.

---

## L-11 — Test the full TUI flow by executing the `tea.Cmd`s

**Crossroads:** the Model tests passed messages with `m.Update(msg)` but ignored the returned `tea.Cmd`. The connection flow never connected in the tests, even though it worked at runtime.

**Decision:** a `step()` helper in the tests executes the returned `cmd` and feeds back the message it produces (like the bubbletea program does). Without this, deferred messages (`ConnectMsg`) never reach the model in the test.

**Lesson:** to test a message architecture you have to simulate the runtime: execute cmds and re-feed their messages. Also, the TUI tests need the engine's blank import (`_ "picklock/internal/store/sqlite"`) or the registry never runs.

---

## L-12 — `tea.BatchMsg` is `[]Cmd` in bubbletea v1.x, not messages

**Crossroads:** the test helper that executed cmds treated `tea.BatchMsg` as a list of messages and passed them to `Update`; the query result was never applied and `loading` stayed `true`.

**Decision:** in bubbletea v1.3.x, `BatchMsg` is `[]Cmd` (commands). The program executes them one by one and delivers each resulting message to `Update`. The test helper now executes each sub-cmd and feeds back its message.

**Lesson:** always verify the actual signature of `tea.Batch`/`BatchMsg` in the installed version. The API changed between v0.x and v1.x.

---

## L-13 — History is lost if each execution creates a new editor

**Crossroads:** when the editor execution was made asynchronous, each query ran in a new `editor.Editor`. The history ended up with a single element because each new editor started empty.

**Decision:** share the `History` pointer between the model's editor and the goroutine's editor (`ed.History = m.editor.History`). The goroutine only mutates the ring buffer; navigation in the UI is guarded with `!m.loading` to avoid races. Verified with `go test -race`.

**Lesson:** when moving an operation to a goroutine, state that must survive between calls (history, counters) has to live in a shared object. Copying the struct doesn't share slices/pointers unless the pointer is copied explicitly.

---

## L-14 — `Ctrl+I`/`Tab` already documented; the pattern repeats (ambiguous control keys)

**Crossroads:** `Ctrl+I` was bound to "structure" but collided with `Tab`.

**Decision:** use `i`. Already documented in L-10. The lesson extends: review the full keymap before implementing to catch ANSI collisions.

**Lesson:** when a SPEC keymap is technically unfeasible, the real TUI is the only way to detect it — test the full flow with simulated keys, not just rendering.

---

## L-15 — SQL Server doesn't support boolean expressions in SELECT

**Crossroads:** mssql's column introspection query used `(c.IS_NULLABLE = 'NO')` directly in the SELECT. Error `Incorrect syntax near '='`.

**Decision:** use `CASE WHEN ... THEN 1 ELSE 0 END` to derive booleans in SQL Server. Also `ISNULL()` instead of `COALESCE()` (both work, ISNULL is the T-SQL idiosyncratic one).

**Lesson:** SQL Server's `information_schema` looks the same as Postgres/MySQL's but the T-SQL syntax differs. Integration tests against real docker containers are the only reliable way to validate introspection queries — and they're fast to run once docker is up.

---

## L-16 — Validate each engine against a real server, not just "it compiles"

**Crossroads:** the engines compiled and the unit-tested dialects passed, but real syntax and introspection bugs only appeared against real servers (docker) — the mssql case above.

**Decision:** per-engine integration tests triggered by env vars (`PICKLOCK_TEST_<MOTOR>_HOST`, etc.) that exercise the whole `Store` interface: tables, columns, constraints, indexes, count, pagination, version. They skip with `t.Skip` if there's no env var, so `go test ./...` always passes.

**Lesson:** the phase-7 done criterion ("connect to each engine in docker") is what really validates the work. Docker is available in this environment: use ephemeral containers (`--rm`) with standard ports.

---

## L-17 — CGO limits cross-compilation; `modernc` enables it

> **Superseded by L-23:** the migration is done; `make release` uses `CGO_ENABLED=0`. Kept as history.

**Crossroads:** `CGO_ENABLED=1` cross-compiling to darwin from linux fails (`clang: unsupported option '-arch'`). The SPEC asked for a multi-platform build.

**Decision:** I verified that `modernc.org/sqlite` (pure-Go driver, registers "sqlite") compiles to `darwin/arm64` with `CGO_ENABLED=0`. Documented in the Makefile as an escape hatch. The linux binary with the 5 drivers weighs 21MB (limit: 40MB).

**Lesson:** when a dependency brings CGO, cross-compiling isn't free. Validate the pure-Go alternative concretely (actually compile it) before promising it in the docs.

---

## L-18 — The password `EchoMode` was applied to the wrong field

**Crossroads:** in the connection form, `EchoMode = EchoPassword` was applied by index `c.fields[2]` (Port) instead of `c.fields[4]` (Password). Silent bug: the password wasn't masked and the port was.

**Decision:** fix the index and add screen tests (`connect_test.go`) that verify per-engine visible fields and masking.

**Lesson:** magic indexes in field slices are fragile. A screen test that validates visible behavior (how many fields, whether it's masked) catches this class of bug that compiles but works wrong.

---

## L-19 — Documentation commands have to be tested, not copied

**Crossroads:** `USAGE.md` had `docker exec maria mysql -uroot -proot -e "CREATE DATABASE test"`. When running it: the `mariadb:11` image doesn't ship the `mysql` binary, only the `mariadb-*` ones (`mariadb`, `mariadb-admin`, ...).

**Decision:** verify every USAGE command by actually running it (sqlite one-liner, 4 `docker run`s, credentials). Fix using `mariadb` and, better, remove the manual step: the `POSTGRES_DB` / `MYSQL_DATABASE` / `MARIADB_DATABASE` env vars make the official images auto-create the database on first boot.

**Lesson:** a command that isn't run in the docs can be broken and nobody notices. The MariaDB container binaries are called `mariadb-*` (not `mysql-*`), and the official images create databases with env vars — use that instead of manual `exec` steps.

---

## L-20 — `docker compose` is the best way to provide test DBs

**Crossroads:** the user asked whether a docker image or a `docker run` is better for "bringing up the database". Four loose `docker run`s are noise and error-prone.

**Decision:** official `compose.yaml`: `docker compose up -d` brings up the 4 engines with fixed credentials, auto-created `test` database, and healthchecks. It was validated: all 4 end up `healthy` and the integration tests pass with those credentials. The per-container `docker run` alternative was kept (with env vars, no `exec`).

**Lesson:** for "bring up the test environment", `docker compose` with healthchecks is the standard, most robust option. A docker image of the TUI itself adds nothing (it needs a TTY and wouldn't include the database); the compose of the databases is what the user needs.

---

## L-22 — A checkbox in a textinput form breaks the focus model

**Crossroads:** adding the `Read-only` toggle (SQLite) and the `SSL` field (PostgreSQL) to the connection form. The toggle isn't a `textinput`, but `ConnScreen` treats all fields the same: `applyFocus()` called `.Focus()` on all of them, and `Update()` passed the key to the active input.

**Decision:** extend `field` with `isToggle bool` + `checked bool`. `applyFocus()` skips toggles (they have no `.Focus()`); `Update()` toggles with `Space`/`Enter` and doesn't forward the key to the input; `renderForm()` draws `[ ]`/`[x]`; `reset()` turns them off. The number of fields per engine changed (sqlite: 2, postgres: 6), so `connect_test.go` was updated and toggle and `sslmode` tests were added.

**Lesson:** when a visual component doesn't fit the base type of the fields (`textinput`), it isn't forced — it's modeled as a variant of the field with its own handler. The real cost is in focus (which element "owns" the key), not in rendering.

---

## L-21 — Makefile: multiline recipe lines break if they don't carry a tab

**Crossroads:** `make demo` failed with "missing separator": the multiline SQL inside the recipe had continuation lines indented with spaces, not tabs.

**Decision:** every recipe line in a Makefile MUST start with a tab. A multiline SQL string with its own indentation breaks parsing. Solution: SQL on a single line per recipe.

**Lesson:** in Makefiles, recipe indentation is a tab (not spaces). To avoid the problem, keep each command on a single line.

---

## L-23 — SQLite switched to `modernc.org/sqlite` (pure Go, no CGO)

**Crossroads:** `mattn/go-sqlite3` requires gcc/CGO. On a machine without a C toolchain the binary **builds** (mattn ships a stub) but SQLite fails at runtime with "go-sqlite3 requires cgo", and every SQLite-dependent test fails. L-04/L-17 documented `modernc.org/sqlite` as an escape hatch, but it was never wired in.

**Decision:** migrate SQLite to `modernc.org/sqlite` (registers driver `"sqlite"`). The DSN changes from mattn params to pragma syntax: `?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)` (read-only: `?mode=ro`). Direct `sql.Open("sqlite3", ...)` callers in tests switch to `"sqlite"`. Remove the gcc requirement from the docs; `make release` now uses `CGO_ENABLED=0`.

**Lesson:** a CGO driver that "builds without CGO" via a stub is a silent failure — the binary compiles but the engine is broken at runtime. Prefer a pure-Go driver when the tool must be a single portable binary. Bonus: on Windows, tests that open a SQLite file must close the store, or `TempDir` cleanup fails because the file stays locked. It was the same with mattn; the stub hid it before.

---

## L-24 — A portable demo generator needs per-engine DDL, not just placeholders

**Crossroads:** extending `cmd/demo` to seed the four network engines from the same dataset as SQLite. The SQLite schema (`INTEGER PRIMARY KEY`, `TEXT`, `REAL`) and `CREATE TABLE IF NOT EXISTS` do not translate directly.

**Decision:** a `internal/demo` package that maps SQLite column types per engine:
- PK autoincrement: `BIGSERIAL` (PG), `AUTO_INCREMENT` (MySQL/MariaDB), `IDENTITY(1,1)` (MSSQL).
- `TEXT UNIQUE` → `VARCHAR(255) UNIQUE` / `NVARCHAR(255) UNIQUE` (TEXT and NVARCHAR(MAX) can't be UNIQUE key columns in MySQL/MSSQL).
- `CREATE TABLE IF NOT EXISTS` doesn't exist in MSSQL → drop tables first, then plain `CREATE TABLE`.
- Reserved words (`key`, `read`) need quoting; quote ALL identifiers per engine (backticks/brackets), mirroring picklock's own `QuoteIdent`.
- Placeholders differ: `?` / `$N` (pgx) / `@pN` (mssql).
- `sql.Open` driver names differ from engine names: mariadb → "mysql", postgres → "pgx", mssql → "sqlserver".

**Lesson:** "portable SQL" is a myth for DDL; the demo needed a small per-engine DDL translator plus identifier quoting. Validated against the real `docker compose` containers (C5).

---

## L-25 — Moving the browser to background ops needs an ID, not just a session token

**Crossroads:** page/table navigation ran synchronously on the UI goroutine, so a slow `COUNT(*)` or page fetch froze the TUI. Making it asynchronous (like the editor) surfaced a staleness bug: after `Esc` cancels a navigation, a new one can start while the old goroutine is still finishing, and the old result would overwrite the new one.

**Decision:** the session token (`queryID`) only changes on connect/session-switch, so it can't distinguish two navigations in the same session. Added `navID`, incremented per navigation dispatch; a `browserDoneMsg` is applied only if its `navID` matches the current one. Also: navigation mutates a `Clone()` of the Browser off-screen and swaps it in on success, so the UI never sees a half-updated browser.

**Lesson:** when moving a stateful operation off the UI goroutine, cancellation and supersession need a per-operation identity, not just a per-session one. Cloning the state for off-screen mutation is a simple way to keep the UI race-free without locking.

---

## L-26 — `SetEscapeHTML(false)` does not reach custom `MarshalJSON` output

**Crossroads:** the JSON exporter promised verbatim values (`a < b` stays `a < b`), but the test found `\u003c`/`\u0026` in the output. The encoder had `SetEscapeHTML(false)` set, yet the escaping persisted.

**Decision:** the row type marshals itself with a custom `MarshalJSON` that escapes each key and value with `json.Marshal` — and `json.Marshal` escapes HTML unconditionally. The encoder's `SetEscapeHTML(false)` only applies to values the encoder encodes; a value already rendered by a `Marshaler` is written verbatim. Fixed by encoding every key/value through a per-value `json.Encoder` with `SetEscapeHTML(false)` (stripping its trailing newline).

**Lesson:** when combining a custom `MarshalJSON` with an encoder that has HTML escaping disabled, the escape policy is inherited from whatever produces the *bytes* — the custom marshaller must disable HTML escaping itself for every sub-value, or the global setting silently does nothing.

---

## L-27 — NULL and empty string are provably indistinguishable from the string value

**Crossroads:** the export tests wanted to confirm that a SQL NULL is exported correctly, and the first version inferred "is null" from `cell == ""`. It failed: both a NULL and an empty string arrive as `""` in `Result.Rows`, which is exactly the ambiguity the new `Result.Nulls` mask exists to resolve.

**Decision:** tests assert the `Nulls` mask directly instead of deriving it from the cell text, and fixtures use a non-empty sentinel (`'x'`) next to the NULL so the two rows differ in their string values too. Export tests then verify the mask drives JSON `null` and empty CSV fields.

**Lesson:** when a model deliberately collapses two values into one representation (NULL → `""`), tests must not re-derive the original from the collapsed value — they must assert the parallel metadata (`Result.Nulls`) or the test becomes a circular tautology.

---

## L-28 — CSV fields are multiline; don't assert CSV by counting lines

**Crossroads:** the CSV quoting test split the output on `\n` to inspect the data row, and got three lines instead of two: `encoding/csv` correctly quotes a value containing a newline, so a single logical record legitimately spans several physical lines.

**Decision:** assert the header prefix and then `strings.Contains` for the expected quoted fragments (`"with, comma"`, `"with ""quotes"""`, `"line...`) instead of line counts. Counting lines is only valid when no field can contain `\n`/`\r\n`.

**Lesson:** when the serialization format has quoting that can span lines (CSV), never validate it by splitting on the record terminator. Test the content the format guarantees (proper quoting around delimiters, quotes doubled), not the physical line layout.

---

## L-29 — Audit the existing keymap before binding a new workspace key

**Crossroads:** the natural binding for "export" was `Ctrl+E`, but it was already taken: `Ctrl+E` is "go to end of line" in the SQL editor (`LineEnd`). Using it would hijack a text-editing keystroke in the editor pane.

**Decision:** bound export to `Alt+E`, following the existing `Alt` prefix convention (`Alt+B` sidebar, `Alt+1..3` panes), which the `textarea`/`textinput` components do not consume. It is captured before the focus switch in `handleWorkspaceKeys`, so it works from any pane without gating on `typingEditor`.

**Lesson:** before adding a binding that must work across panes (including the editor), read the full keymap and the components' own reserved keys (`Ctrl+E`, `Ctrl+A`, `Ctrl+L`, `Ctrl+R`, `Tab`...). Prefer the `Alt` prefix for cross-pane actions: it never collides with the text-editing control keys.

---

## L-30 — A DSN parser can't guess the scheme by "://"

**Crossroads:** `picklock <dsn>` had to tell a SQLite path (`./app.db`, `/abs/db.sqlite`) apart from a network URL (`postgres://...`). The obvious heuristic — `strings.Contains(dsn, "://")` — misclassifies `sqlite:picklock.db` and `file:picklock.db` (no `://`), and would glue a `?mode=ro` query onto a `file:/path?mode=ro` path.

**Decision:** parse every argument with `net/url` and switch on `u.Scheme`: `postgres`/`mysql`/`mariadb`/`sqlserver` → network engines, `sqlite`/`file` → SQLite, and **empty scheme** → a plain SQLite path. For relative URIs (`sqlite:picklock.db`) the path lives in `u.Opaque`, not `u.Path`; absolute ones use `u.Path`. And the database location differs per engine: SQL Server puts it in the `?database=` query (its URL format has no path database), the rest in the path.

**Lesson:** URL-ish strings lie on the edge cases between "has a scheme" and "is a path"; write the parser against the actual `net/url` fields (Scheme/Path/Opaque) and per-engine quirks, tested with the exact strings you document, instead of a substring heuristic.

---

## L-31 — "Read-only" exists per driver, not as a portable DSN flag

**Crossroads:** the global `--read-only` had to work for all five engines. SQLite has `mode=ro`, PostgreSQL has `default_transaction_read_only` in the DSN, MySQL/MariaDB have **nothing** at the DSN level, and SQL Server has no per-session read-only toggle at all.

**Decision:** enforce where each driver allows it and be honest where it does not:
- MySQL/MariaDB: `SET SESSION TRANSACTION READ ONLY` is per-connection, and the `database/sql` pool hands statements to arbitrary connections. `Exec`/`ExecContext` in read-only mode pin one connection (`db.Conn`), set the session var there, and run the statement on it — the server rejects the write. Reads stay on the normal pool.
- SQL Server: the driver has no server-side read-only session mode, so picklock now enforces read-only at the application level — `IsSQLWrite` blocks `INSERT`/`UPDATE`/`DELETE`/`CREATE`/`DROP`/etc. when `read_only` is set, mirroring the Redis/Cassandra guards.

**Lesson:** a feature named the same across engines is really per-engine behavior; map it to each driver's actual mechanism and surface the gap visibly — for MSSQL the gap is now closed by an app-level guard instead of a warning.

---

## L-32 — `encrypt=optional` is NOT "TLS if available"; prefer ≠ optional in mssql

**Crossroads:** mapping the form's `prefer` to go-mssqldb's `encrypt=optional` sounded right, but the integration test against the compose SQL Server failed: the handshake tried TLS (the container supports it), then failed cert verification (self-signed, invalid for `localhost`) and **did not fall back**.

**Decision:** `prefer` keeps the driver default for SQL Server instead of setting `encrypt=optional` — the default encrypts when the server supports it without demanding a trusted certificate, so local/dev servers with self-signed certs connect, and it still uses TLS against servers that require it. Only `require` (encrypt=mandatory, verified) and `disable` (encrypt=disable) set explicit params. MySQL's driver makes the three-way split honest (`preferred`/`true`/`false`), and the README/05 table documents the per-engine meaning.

**Lesson:** behind a portable value like `prefer`, each driver has a subtly different "try TLS" behavior — optional-with-fallback, default-without-validation, negotiate-if-supported. When the mapping is a guess, the real-container integration test is the arbiter: it caught a mapping that "compiled correctly" but broke local dev connections.

---

## L-33 — A new persisted file turns existing tests into config writers

**Crossroads:** adding the persistent query history (writes to `~/.config/picklock/history.json`) meant any test that ran a query silently wrote to the **real user config dir** — until then the TUI tests only *read* `PICKLOCK_CONFIG_DIR` and scattered `t.Setenv` per test was enough.

**Decision:** added a package-level `TestMain` that points `PICKLOCK_CONFIG_DIR` at a throwaway `os.MkdirTemp` dir for the whole tui test binary. It is hermetic (saved connections, prefs and history are all isolated) and it removes the per-test `Setenv` noise going forward. The file is written only on the UI goroutine, immediately next to the `History.Push` it mirrors, so there is no file race with the query goroutines.

**Lesson:** when a feature starts *writing* a user file that tests previously only read, the fix is to isolate the config root once for the entire test binary — not to sprinkle more `t.Setenv` lines in individual tests (a future test author will forget one).






## L-34 — A cosmetic change to the table separator rewrites the column-width contract

**Crossroads:** restyling the data table from `id  name  email` (one space) to
`id | name | email` (three chars per separator) looked like a trivial string
change, but `colWidths` had the separator cost hardcoded as `n-1` cells and the
tests asserted the widths against that contract. Two tests broke in a subtle
way: the "must fit below the floor" one became **physically impossible** (12
columns at 1 cell each + 11 separators of 3 cells = 45 > 40).

**Decision:** made the separator an explicit `colSep`/`colSepW` constant used by
the width math, added the underline line under the header, and updated the
geometry tests to the new contract: tables shrink to a 1-cell minimum and the
hard physical floor is now `n + (n-1)*3` — a table wider than that simply
cannot fit and the last-resort loop stops there instead of overflowing.

**Lesson:** any change that touches *visual width* (separators, padding, border
runes) is a change to the layout contract — grep for the hardcoded `n-1` and the
tests that recompute totals, and keep a single source of truth (the `colSepW`
constant) so the renderer and the tests can never disagree again.

## L-35 — Styling a third-party component: read its exported Style struct before hacking

**Crossroads:** the SQL editor needs a line-number gutter with a background
chip and a highlighted cursor line. The natural instinct was to render the
editor view ourselves — but `bubbles/textarea` already exposes `FocusedStyle`
/ `BlurredStyle` with `LineNumber` and `CursorLineNumber` fields that it applies
in its own `View()`. Reading the dependency's source paid for itself.

**Decision:** set `ta.FocusedStyle.LineNumber`, `.CursorLineNumber` and the
blurred counterparts to the project's styles instead of reimplementing the
textarea rendering. `lipgloss.Style.Inline(true)` (applied by the library) keeps
the width math correct even with padding.

**Lesson:** before overriding or reimplementing a third-party widget's
rendering, check whether it exposes its internal styles — the library's exported
`Style` struct is the extension point, keeps the renderer agreement intact
(View-width math, hit-testing), and avoids the distracting "syntax highlighter"
rabbit hole for a component that deliberately renders plain text.

## L-36 — Border removal is a layout contract change: watch the anchoring tests

**Crossroads:** making the connection form fit on small terminals meant dropping
the 3-line bordered inputs for one-line `[ value ]` boxes. The screen got 2×
shorter and even SQL Server's seven fields now fit at 20 rows — but a test that
parsed the old box borders (`╭`, `│`) to check horizontal centering broke in two
ways: the delimiters changed, and the engine row's `←→ switch` hint now hangs
off the closing bracket, so the brackets were no longer the visual anchor.

**Decision:** the tests now anchor on the new bracket delimiters and measure the
full visual element (first `[` to last visible cell, since the hint is part of
the box). All form boxes use a single `fieldBoxW` constant so every row's box
starts at the same column — the engine hint lives *inside* the fixed-width box
rather than shifting its center.

**Lesson:** changing a screen's chrome (borders → plain text) changes the
tokens that layout tests parse. Grep the tests for the old visual markers and
update them to the new anchor, and centralize the new geometry in a constant
(`fieldBoxW`) so the renderer and the tests share the same truth.

## L-37 — bounded input escapes break fixed-width layouts; geometry must come from the render

**Crossroads:** forcing every form box to 32 cells with `fmt.Sprintf("[ %-28s ]", value)`
worked for the engine and the toggle, but the text fields rendered 28 wide and
their labels drifted 2 cells right. The culprit: `textinput.View()` returns
ANSI-colored placeholder text, so its *byte length* (54) and *runewidth* (which
counts the printable chars inside the escape) both exceed the 24 visible cells —
the `%-28s` padding silently did nothing.

**Decision:** pad by display width using `lipgloss.Width` (ANSI-aware: 24 visible
cells) instead of byte/runewidth counts, so the bracketed box is truly 32 cells.
The clickable rows are no longer index arithmetic that must mirror the render
by hand: `View` scans the lines it just emitted (stripping ANSI) and records the
engine/field/button/saved rows at the exact position they will occupy, then the
mouse hit-test reads that list — renderer and hit-test cannot drift apart.

**Lesson:** when a fixed-width visual contains third-party widgets that emit
styled text, the only trustworthy measure of its size is a display-width
function that understands ANSI (lipgloss.Width), never len() or runewidth on the
raw string. And layout tests should assert the anchor the design actually uses
(the label+box group, now that labels are left-aligned) rather than an earlier
visual token like a border.
