# Security — quick guide for `picklock` users

`picklock` runs in your terminal and connects to databases **you** choose. Everything that goes through `picklock` stays on your machine and in the database you connect to. Here's what you need to know.

## What it saves and where

When you save a connection with `Ctrl+S`, `picklock` writes `~/.config/picklock/connections.json`:

- `0600` permissions (only your user can read it).
- If you check **save password**, it's stored **in plaintext** in that file.
- That's why: **don't save the password on a shared machine**, and don't leave the file in unencrypted backups. If in doubt, leave the password field empty when saving: `picklock` will ask for it when connecting.

Your last 100 queries are kept in `~/.config/picklock/history.json` (also `0600`).
A query can contain an inline credential (`USE ... PASSWORD`, `IDENTIFY BY`);
the same rule applies: don't run queries with secrets in them on a shared
machine. The config directory can be overridden with the `PICKLOCK_CONFIG_DIR`
env var.

## Connecting securely

- **Use the `SSL` field for every network engine.** By default it's `prefer` (uses TLS if the server offers it). For production databases, set `require` or `verify-full` (the latter also validates the certificate).
- **`Read-only` mode for every engine.** If you're only going to look, enable the `Read-only` toggle: any write fails instead of modifying the data (for SQLite, the file is opened read-only).
- **Use a database user with minimal permissions.** `picklock` uses the credentials as-is: connect with a user that only has what you need, not the default `root`/`sa`/`admin`.

## Destructive queries

`picklock` **doesn't ask for confirmation** for `DROP`, `TRUNCATE`, `UPDATE`/`DELETE` without `WHERE` — it assumes you know what you're doing. Before running something destructive against a database that matters:

- Check in the header which database you're connected to (engine, host, database).
- Open it in `Read-only` if you're only going to look.
- Write the query with a `WHERE` first, run a `SELECT` of the affected rows, and only then the `UPDATE`/`DELETE`.

## Sensitive data

- There's no logging to file: errors are shown on screen and gone when you exit.
- Saved passwords and query history are stored in plaintext files (`0600`)
  under `~/.config/picklock/` — see "What it saves and where" above.
- The engine protects network traffic: for connections to external servers, prefer TLS (`SSL` field).

---

Technical details and threat model for maintainers: `06-security.md`.
