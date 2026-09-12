# 01 — Vision and philosophy

## Project name

`picklock` — terminal database browser.

## Defining phrase

> The database is already there. You don't need an app. You need a window.

## What it is

`picklock` is a terminal tool (TUI) written in Go to explore, query and edit databases. It runs entirely in the terminal: no GUI, no server, no prior configuration.

It supports exactly five engines — SQLite, PostgreSQL, MySQL, MariaDB and SQL Server. The user picks the engine on a connection screen, fills in the form (or uses a saved connection), and can navigate tables, run arbitrary SQL and view results — all from the keyboard.

## What it is NOT

- It is not an ORM or a library.
- It does not support Oracle, DB2, Snowflake, SAP HANA, MongoDB, Redis or any other engine. Only the five listed, and there are no plans to add more.
- It is not a GUI with a native window.
- It does not try to replace complex tools like DBeaver or TablePlus.
- It has no mouse-first mode. The keyboard is the primary interface.

## Inspiration

Inspired by [DBee](https://github.com/murat-cileli/dbee) but with its own identity:

| DBee | picklock |
|---|---|
| Multiple engines | Exactly five engines: SQLite, PostgreSQL, MySQL, MariaDB, SQL Server |
| Tabs per engine on connection | A form that changes according to the selected engine |
| Browser mostly | Browser + first-class SQL editor |
| Go + tview | Go + bubbletea + lipgloss |

## Design principles (non-negotiable)

1. **Five engines, one binary.** SQLite, PostgreSQL, MySQL, MariaDB and SQL Server. No more. The `Store` interface is written once and each engine implements it with its own dialect. The UI, the browser and the editor never know which engine they are talking to.

2. **One connection, one session.** The user connects through the connection screen and works. To change databases, open another terminal or return to the connection screen with `Ctrl+N`. There are no simultaneous sessions in tabs.

3. **Keyboard first.** Every action has a keyboard shortcut. The mouse may work but is never required.

4. **No unnecessary abstraction.** The user runs real SQL, sees real results. There is no "visual mode" that hides the query.

5. **Lightweight.** The compiled binary must not exceed ~40 MB. No external runtime dependencies. The weight of the five drivers is the accepted cost of a single multi-engine binary.

6. **Fail loudly.** If something goes wrong (connection refused, invalid query, wrong credentials, corrupt file), the error is visible, clear and actionable. Errors are never silenced.

7. **Readable code over clever code.** The agent must prioritize clarity. Short functions, descriptive names, comments where the "why" is not obvious.

## Target user

A developer who already lives in the terminal. Uses `vim`/`neovim`, `tmux`, `git` from the CLI. Knows SQL. Doesn't want to open a GUI app to look at a 50-row table, nor learn a different client per engine.
