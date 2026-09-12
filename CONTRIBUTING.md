# Contributing to picklock

Thanks for taking the time to contribute. This document explains how to set up
the project, run the checks, and what is expected from a contribution.

## Requirements

- **Go 1.26.6+** (see `go.mod`).
- No CGO, gcc or libsqlite needed: the SQLite driver (`modernc.org/sqlite`) is
  pure Go. Tests use only the Go standard library `testing` package.
- Optional: **Docker** for the network engine integration tests
  (PostgreSQL, MySQL, MariaDB, SQL Server).

## Setting up

```bash
git clone https://github.com/agmonetti/picklock
cd picklock
```

There are no dependencies to install manually; `go` fetches them from the module
cache.

## Common tasks

```bash
go build ./...        # compile everything
go vet ./...          # lint (also: make lint)
go test ./...         # run the unit tests (also: make test)
go run ./cmd/demo     # create demo.db with 20 example tables (no server needed)
go run ./cmd/picklock     # run the TUI
```

The `Makefile` wraps most of these (`make build`, `make test`, `make lint`,
`make demo`, ...).

## Integration tests (network engines)

The unit tests skip the network engines when their environment variables are
not set, so `go test ./...` passes without Docker. To exercise the network
drivers:

```bash
docker compose up -d --wait

PICKLOCK_TEST_POSTGRES_HOST=localhost PICKLOCK_TEST_POSTGRES_USER=postgres \
PICKLOCK_TEST_POSTGRES_PASSWORD=postgres PICKLOCK_TEST_POSTGRES_DATABASE=test \
PICKLOCK_TEST_MYSQL_HOST=localhost PICKLOCK_TEST_MYSQL_USER=root \
PICKLOCK_TEST_MYSQL_PASSWORD=root PICKLOCK_TEST_MYSQL_DATABASE=test \
PICKLOCK_TEST_MARIADB_HOST=localhost PICKLOCK_TEST_MARIADB_USER=root \
PICKLOCK_TEST_MARIADB_PASSWORD=root PICKLOCK_TEST_MARIADB_DATABASE=test \
PICKLOCK_TEST_MSSQL_HOST=localhost PICKLOCK_TEST_MSSQL_USER=sa \
PICKLOCK_TEST_MSSQL_PASSWORD='Str0ng!Passw0rd' PICKLOCK_TEST_MSSQL_DATABASE=master \
go test -timeout 300s ./internal/store/...

docker compose down
```

The same command set runs in CI on every push and pull request.

## Code style

- Format with `gofmt` (the project is `gofmt`-clean; run `gofmt -l .` and fix
  any output before committing).
- Follow the existing structure: `internal/store` is the only place allowed to
  import `database/sql`; the `Store` interface is engine-neutral by design.
  Don't leak driver-specific types into the UI layers.
- Prefer readable, short functions over clever ones. Comments explain the
  "why" when it is not obvious.
- Don't over-engineer; the project philosophy is the simplest option that
  fulfills the functionality (see `docs/design/01-vision.md`).

## Tests

- Add or update tests for the code you change. Tests use the standard library
  `testing` package, matching the rest of the repo.
- Table-driven tests are the norm where inputs vary.
- New engine-level behavior should be covered by an integration test guarded by
  its `PICKLOCK_TEST_<ENGINE>_HOST` env var (they skip when unset).

## Commit messages

Commit messages follow [Conventional Commits](https://www.conventionalcommits.org/):

```
<type>(<scope>): <short summary>
```

Types used in this repo: `feat`, `fix`, `docs`, `perf`, `refactor`, `style`,
`chore`, `ci`. Examples from the history:

- `feat(store): cap editor query results to 10k rows`
- `fix(ui): reset the port to the new engine default on switch`
- `ci: run govulncheck on every push and pull request`

## Submitting a pull request

1. Fork the repo and create a branch with a descriptive name.
2. Make your change and commit it with a Conventional Commits message.
3. Run `gofmt -l .`, `go vet ./...` and `go test ./...` — all must pass.
   If you touched a network engine, run its integration tests too.
4. Open a pull request describing what changed and why. Reference the issue if
   there is one.

## Reporting bugs and vulnerabilities

- For bugs and general questions, open a GitHub issue.
- For security vulnerabilities, **do not** open a public issue. Use GitHub's
  private vulnerability reporting instead — see `SECURITY.md`.
