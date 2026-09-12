# picklock — documentation for the agent

This directory contains everything you need to implement `picklock` without asking any questions.

## The core idea (non-negotiable)

`picklock` **is not a SQLite browser**. It is a terminal database browser that supports exactly five engines:

| Engine | Go package |
|---|---|
| **SQLite** | `modernc.org/sqlite` |
| **PostgreSQL** | `github.com/jackc/pgx/v5` |
| **MySQL** | `github.com/go-sql-driver/mysql` |
| **MariaDB** | `github.com/go-sql-driver/mysql` |
| **SQL Server** | `github.com/microsoft/go-mssqldb` |

**And no more.** No Oracle, no DB2, no Snowflake, no NoSQL. Every design decision — the `Store` interface, the connection screen, the dialects, the implementation phases — exists to serve these five engines with a single binary and a single UI.

## Reading order

| File | Contents |
|---|---|
| `01-vision.md` | What it is, what it is not, non-negotiable principles. Read first. |
| `02-architecture.md` | Directory structure, layers, `Store` interface, dialects per engine. |
| `03-ux-screens.md` | Connection screen, layout, screens, keymaps, styles, error messages. |
| `04-implementation.md` | Implementation phases with verifiable done criteria. |
| `05-technical-decisions.md` | DSNs per engine, edge cases, dialects, saved connections, performance. |
| `06-security.md` | Threat model, security decisions and backlog (maintainers). |
| `07-user-security.md` | Security for the end user. |

Guides for the end user (not for the agent):

| File | Contents |
|---|---|
| `../../USAGE.md` | First use step by step: whether to start servers, how to test each engine, how to run queries. |
| `../../README.md` | Project summary, installation and shortcuts. |

## Main instruction

Implement `picklock` following the documents in order. Respect the phases of `04-implementation.md` — each phase has a done criterion that must pass before moving on.

If something is not specified, apply the principle of `01-vision.md`: the simplest and most readable option that fulfills the functionality. No over-engineering.

## Final stack

- **Language:** Go 1.26.6+
- **TUI:** bubbletea + lipgloss + bubbles (Charmbracelet)
- **Engines:** SQLite, PostgreSQL, MySQL, MariaDB, SQL Server (drivers listed above)
- **Testing:** stdlib `testing`, no external frameworks
- **Test environment:** `compose.yaml` starts the 4 network engines with fixed credentials and the `test` database auto-created (`docker compose up -d`). `make demo` creates an example SQLite database without docker.
- **Build:** `go build ./cmd/picklock/`
