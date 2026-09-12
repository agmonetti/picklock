# 05 — Limitations & MVP Boundaries

## 1. Scope and Honesty

This document transparently records the design tradeoffs, intentional omissions, and MVP boundaries for the non-relational database expansion in `picklock`.

---

## 2. Engine-Specific Limitations

### 1. MongoDB
- **MQL Language Scope**: The MongoDB query editor supports direct JSON query filters (e.g. `{"status": "active"}`), aggregation pipeline arrays (`[{"$match": ...}]`), find wrapper syntax (`find(...)`), and database commands (`{"collStats": "users"}`). It does **not** embed a full JavaScript engine (e.g. no arbitrary JS functions, `forEach` loops, or complex map-reduce scripts).
- **Index Management**: Indexes can be inspected (`i` key) but creating/dropping indexes is done via the query editor using commands rather than dedicated UI modals.
- **Transactions**: Multi-document replica set transactions (`startSession`) are not exposed as interactive state machines.

### 2. Redis
- **Streams (XREAD / XGROUP)**: The Redis MVP supports Strings, Hashes, Lists, Sets, and Sorted Sets. Redis Streams (`XADD`, `XREAD`) are queryable via the command editor, but dedicated consumer group stream timelines are deferred.
- **Cluster Mode**: Connects to standalone Redis instances or single cluster nodes. Automatic multi-node cross-slot cluster redirection is deferred to future work.
- **Key Pattern Discovery**: Key catalog browsing uses cursor `SCAN` with a default batch limit (100 items per page) to prevent server stalls on multi-million key databases.

### 3. Apache Cassandra
- **Keyspace Selection**: Connection connects to a specified keyspace. Switching keyspaces within a live session is done by returning to connection (`Ctrl+N`) or via `USE keyspace` in CQL.
- **Secondary Indexes & Materialized Views**: Viewable in structure inspection (`i`), but complex SASI index configurations are viewed as CQL schema.

### 4. Neo4j
- **Textual Graph Explorer vs Graphical Visualizer**: The TUI provides textual node inspection and interactive incident relationship traversal in the detail view (`v`). Full 2D/3D graphical ASCII graph topology rendering is deferred.
- **Path Queries**: Cypher queries returning paths render the nodes and relationships contained in the paths.

---

## 3. General Cross-Cutting Limitations

1. **Inline Cell Mutation**: Direct spreadsheet-like in-place cell editing is out of scope; mutations are executed via the query editor.
2. **OS Keychain**: Password persistence remains in `0600` `~/.config/picklock/connections.json`. OS keychain integration (libsecret / macOS Keychain) remains in the roadmap.
3. **Multi-Tab Sessions**: `picklock` remains single-connection, single-window. To view two databases concurrently, open a separate terminal or tmux pane.

---

## 4. Future Roadmap

1. **Stage 1 (Completed in this branch)**: MongoDB (Document) & Redis (Key-Value).
2. **Stage 2 (Completed in this branch)**: Apache Cassandra (Wide-Column).
3. **Stage 3 (Completed in this branch)**: Neo4j (Graph).
4. **Post-v0.2.0**:
   - Stream timeline viewer for Redis Streams.
   - Elasticsearch / OpenSearch adapter.
   - DynamoDB adapter.
   - OS Keychain credential storage.
