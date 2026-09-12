# 03 — UX, screens and keymaps

## Screen 0: Connection

It is the first screen when opening the tool. lazyvim style: logo centered on top, and below it the form and the saved connections, all centered in the available area. Each field is a row `label (left) + one-line bracketed box`, and there is a bottom button bar with distinguishable actions.

```
┌─────────────────────────────────────────────┐
│ picklock [no connection]                        │
├─────────────────────────────────────────────┤
│                _____  ______  _      __  __ │
│               |  __ \|  ____|| |    |  \/  |│
│               | |__) | |__   | |    | \  / |│
│               |  _  /|  __|  | |    | |\/| |│
│               | | \ \| |____ | |____| |  | |│
│               |_|  \_\______|_\_____|_|  |_|│
│                                             │
│                    Connect                  │
│                                             │
│            Engine [ sqlite   ←→ switch  ]  │
│            File   [ /data/app.db         ]  │
│            Read-only [ ] open read-only     │
│                                             │
│                Enter · Connect              │
│          ctrl+s save   r clear              │
├─────────────────────────────────────────────┤
│ [↑↓] list  [tab] engine  [enter] connect    │
└─────────────────────────────────────────────┘
```

When choosing another engine, the form changes:

```
│  Engine  [ PostgreSQL ▾ ]                   │
│  ─────────────────────                       │
│  Host     [ localhost ]                      │
│  Port     [ 5432 ]                           │
│  User     [ postgres ]                       │
│  Password [ •••••••• ]                       │
│  Database [ mydb ]                           │
│  SSL      [ prefer ]                         │
│                                              │
│  [ Connect ]                                 │
```

**Behavior:**
- The engine selector offers exactly: **SQLite, PostgreSQL, MySQL, MariaDB, SQL Server**. No others.
- The form fields are one-line bracketed boxes (`[ localhost ]`) of a fixed
  width, so every engine — even SQL Server with its seven fields — fits on a
  standard 24-row terminal. On very short terminals (< 22 rows) the ASCII logo
  is hidden and only the form is drawn.
- SQLite shows the `File` field and the `Read-only` toggle (toggled with `Enter`/`Space`; opens the file in `mode=ro`).
- The other engines show `Host`, `Port`, `User`, `Password`, `Database`.
- All network engines show the `SSL` field (`prefer`, `require`, `disable`;
  PostgreSQL adds `verify-ca`/`verify-full`). Invalid values block the
  connection with an error.
- `Enter` connects. Connection errors are shown in the footer in red, without crashing.
- `Ctrl+S` saves the current connection to the local history (see `05-technical-decisions.md`).
- Saved connections are listed below the form (only if there are any); `Enter` on one loads and connects it.
- All labels are left-aligned in a fixed column and every box starts at the
  same cell, so the whole form reads as aligned columns.
- The mouse works on the form: a left click focuses the clicked field / engine
  selector, toggles the `Read-only` checkbox, triggers Connect from the button
  or opens the clicked saved connection.
- Connecting resets the previous session: it closes the previous store, creates the new one, and loads the browser.
- `Ctrl+N` on any screen returns to connection (new session).

## General layout (work screens)

The terminal is divided into fixed zones:

```
┌─────────────────────────────────────────────┐
│ HEADER (1 line)                             │
│ picklock [ postgres@localhost:5432/mydb ] [ users ] [ browser ] │
├──────────────┬──────────────────────────────┤
│              │ MAIN PANE (browser/structure)│
│  SIDEBAR     │                              │
│  (tables)    ├──────────────────────────────┤
│  ~1/4 width  │ SQL EDITOR (input + results) │
│              │ ~1/3 height                  │
├──────────────┴──────────────────────────────┤
│ FOOTER (1 line)                             │
│ [contextual shortcuts]          [page X/Y]  │
└─────────────────────────────────────────────┘
```

After connecting there is a **single window**: the sidebar, the main pane and
the SQL editor are always visible, each inside its own border. `Tab` moves the
keyboard focus between panes (sidebar → main → editor); the focused pane has an
accent border. There is no screen switching.

- The header is always visible. Its values are bracketed **pills**: the app
  name, then a yellow pill for the connection and a purple pill for the active
  table, with black text on a solid background:
  - Network: `picklock [ postgres@localhost:5432/mydb ] [ users ]`
  - SQLite: `picklock [ sqlite /data/app.db ] [ users ]`
  - No connection / no open table: the pill stays muted text without a background.
  - There is no mode pill: the focused pane already marks itself with its accent
    border, so `[ browser ]` / `[ tables ]` / `[ editor ]` would be redundant.
- The sidebar lists the database tables (scrolled when there are many). It can be hidden with `Alt+B` and is hidden automatically below 60 columns.
- The main pane shows the active table (browser) or its structure.
- The editor pane is the SQL editor with its results.
- The footer shows the most relevant shortcuts for the focused pane: the
  pressing keys are bracketed (`[Tab]`) and bold, the action follows in muted
  text. The `?` help cheatsheet uses the same bracket format, tab-aligned.

## Pane 1: Sidebar (tables)

```
│ > users      │
│   orders     │
│   products   │
│   sessions   │
```

- `↑↓` / `j k` move a selection cursor over the table list (it scrolls).
- `Enter` opens the selected table in the main pane. `>` marks the cursor;
  the opened table is highlighted in accent.
- `1..9` quick selection by index.
- `PgUp` / `PgDn` move the cursor in steps of 10.

## Pane 2: Main (browser or structure)

**Browser** (default). Shows 50 rows per page (configurable in the future);
the ID column (if it exists) is always the first one.

```
│ id | name        | email        │
│ ---+-------------+------------- │
│  1 | Alice       | a@test.com   │
│  2 | Bob         | b@test.com   │
│  3 | Carol       | c@test.com   │
```

- Column widths are calculated automatically to the maximum of the visible content, without exceeding the available space. The columns are separated by
  ` | ` and a dashed line connects them under the header.
- An empty table keeps the header and the separator and draws a centered
  empty-table ASCII box with `( 0 rows returned )` underneath; the pane title
  shows `users · (empty)`.
- `NULL` values are shown as `∅` in dim gray.
- Very long values are truncated with `…` to the column width.
- `↑↓` / `j k` navigate rows; `PgUp`/`PgDn` (or `Ctrl+U`/`Ctrl+D`) change page; `g`/`G` go to the first/last row.
- `r` reloads the table (after a write query it reloads automatically).
- `i` switches the main pane to the structure; `Esc` returns to the browser.

**Structure**: columns with type and constraints (PK, NN, DEF) and indexes
(name, columns, UNIQUE).

```
│ Columns                      │
│ ─────────────────────────── │
│ id       INTEGER  PK  NN     │
│ name     TEXT         NN     │
│ email    TEXT         NN     │
│                             │
│ Indexes                     │
│ idx_email  (email)  UNIQUE   │
```

## Pane 3: SQL editor

Always visible. The input area occupies the upper part of the pane, the results
appear below, separated by a line.

- `Ctrl+R` runs the query under the cursor.
- If the query returns rows, it shows the results in a table; a write statement
  shows `STATUS: OK (N rows affected, 1.2ms)` with the measured duration; an SQL error shows the engine's literal message in red, without crashing.
- The input shows a styled line-number gutter (the cursor line is highlighted).
- `↑↓` on the first/last line of an empty input navigates the history (last 100 queries, ring buffer).
- The input supports multiple lines with `Enter`. `Ctrl+R` always runs the entire buffer.
- `Ctrl+L` clears the input. `Esc` returns the focus to the main pane.
- After a write query (INSERT/UPDATE/DELETE/CREATE/...), the sidebar and the open table auto-refresh.


## Full keymap

### Global (available on any screen)

| Key | Action |
|---|---|
| `Ctrl+C` / `q` | Quit |
| `Ctrl+N` | New connection (closes the current session) |
| `Ctrl+S` | Save current connection (on the connection screen) |
| `Alt+B` | Toggle sidebar |
| `?` | Toggle help panel |
| `Tab` | Move focus to the next pane (sidebar → main → editor) |
| `Alt+1` / `Alt+2` / `Alt+3` | Jump to sidebar / main / editor |
| `Alt+E` | Export (prompt to save the result / current table page) |
| `Alt+C` / `Ctrl+Y` | Copy query editor buffer to clipboard |

> `Alt+1..3` work on every terminal. `Ctrl+1..3` are also bound for terminals
> with CSI-u support (kitty, wezterm), but `Ctrl+<digit>` is not distinguishable
> from `<digit>` in classic terminals, so `Alt` is the reliable choice.

### Connection screen

| Key | Action |
|---|---|
| `↑` / `↓` | Navigate list of saved connections (left panel) |
| `Tab` | Switch between form fields |
| `←` / `→` | Switch engine (on the engine selector) |
| `Enter` | Connect |
| `Ctrl+S` | Save connection |
| `d` | Delete the selected saved connection (on the saved list) |
| `r` | Reset form to blank |

### Sidebar

| Key | Action |
|---|---|
| `↑` / `k` | Previous table |
| `↓` / `j` | Next table |
| `PgUp` / `PgDn` | Move 10 tables |
| `Enter` | Open the selected table and focus the main pane |
| `1..9` | Select table by index |

### Main pane (browser)

| Key | Action |
|---|---|
| `↑` / `k` | Previous row |
| `↓` / `j` | Next row |
| `PgUp` / `Ctrl+U` | Previous page |
| `PgDn` / `Ctrl+D` | Next page |
| `g` / `Home` | First row |
| `G` / `End` | Last row |
| `r` | Refresh (reloads table; auto after a write query) |
| `i` | Show structure |
| `Esc` | Back to browser (from structure) |

### Editor

| Key | Action |
|---|---|
| `Ctrl+R` | Run query |
| `Alt+C` / `Ctrl+Y` | Copy query buffer to clipboard |
| `↑` (on empty input) | Previous query in history |
| `↓` (on empty input) | Next query in history |
| `Ctrl+L` | Clear input |
| `Ctrl+A` | Go to start of line |
| `Ctrl+E` | Go to end of line |
| `Esc` | Back to the main pane |

### Export prompt (`Alt+E`)

`Alt+E` from the workspace opens a centered prompt with a single text input for
the target file name, pre-filled with `picklock-export-<timestamp>.csv`. The format
is taken from the extension (`.json` → JSON, otherwise CSV). `Enter` writes the
file and closes the prompt showing `exported N rows → /abs/path` in green;
`Esc` cancels. Data source: the last query result when the editor is focused,
the current page of the active table otherwise. SQL NULL is exported as JSON
`null` (empty field in CSV).

## Styles and colors

Use a palette adaptive to the terminal theme (don't hardcode RGB colors). Lipgloss with `lipgloss.AdaptiveColor`:

```go
var (
    ColorPrimary   = lipgloss.AdaptiveColor{Light: "#1D9E75", Dark: "#5DCAA5"} // teal
    ColorAccent    = lipgloss.AdaptiveColor{Light: "#534AB7", Dark: "#AFA9EC"} // purple
    ColorMuted     = lipgloss.AdaptiveColor{Light: "#888780", Dark: "#B4B2A9"} // gray
    ColorError     = lipgloss.AdaptiveColor{Light: "#A32D2D", Dark: "#F09595"} // red
    ColorNull      = lipgloss.AdaptiveColor{Light: "#B4B2A9", Dark: "#5F5E5A"} // dim gray
    ColorBorder    = lipgloss.AdaptiveColor{Light: "#D3D1C7", Dark: "#444441"} // border gray
)
```

- Header: `ColorPrimary` bold.
- Active table in sidebar: `ColorAccent`.
- Errors: `ColorError`.
- NULL: `ColorNull` with `∅` text.
- Separator borders: `ColorBorder` with `lipgloss.Border(lipgloss.NormalBorder())`.
- Footer: `ColorMuted`.

## Loading states

When a query takes longer than 100ms, show in the footer:
```
running query…
```
Using `tea.Tick` with a bubbletea spinner (`bubbles/spinner`). Don't block the event loop.

## Error messages

Never crash with `panic`. Always show the error in the footer or in the results area:

- Connection error: in footer, red. Example: `cannot connect to localhost:5432: connection refused`
- Wrong credentials: engine's literal text: `password authentication failed for user "postgres"`
- SQL query error: in results area, red. Engine's literal text.
- Empty table: a centered empty-table ASCII drawing with the caption `( 0 rows returned )`
- Database without tables: `no tables — use the editor to create one`
- Database without permission to list: engine's literal text.
