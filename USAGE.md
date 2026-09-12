# USAGE — first steps, step by step

`picklock` **does not start servers or create files on its own**. It connects to
databases that **already exist and are running** — you bring the database, `picklock`
opens a window to look at it, browse its data, inspect its schema, and query it.

- **SQLite** = a `.db` file on your disk (no server). You create it yourself or with `make demo`.
- **PostgreSQL / MySQL / MariaDB / SQL Server** = relational servers.
- **MongoDB** = document database.
- **Redis** = in-memory key-value data store.
- **Apache Cassandra / ScyllaDB** = wide-column distributed database.
- **Neo4j** = graph database.

The repo ships a `compose.yaml` to start all network engines at once for testing:

```bash
docker compose up -d
```

---

## 0. Quick Start

### Build the binary

```bash
go build -o picklock ./cmd/picklock
./picklock
```

### Skip the connection screen with a DSN

```bash
# Relational
picklock ./db.sqlite                                        # SQLite file
picklock postgres://postgres:postgres@localhost:5432/test   # PostgreSQL
picklock mysql://root:root@localhost:3306/test             # MySQL
picklock mariadb://root:root@localhost:3307/test           # MariaDB
picklock 'sqlserver://sa:Str0ng!Passw0rd@localhost:1433?database=master'

# Non-Relational
picklock mongodb://localhost:27017/test                    # MongoDB
picklock redis://localhost:6379/0                          # Redis
picklock cassandra://localhost:9042/picklock_demo              # Cassandra
picklock neo4j://neo4j:password@localhost:7687/neo4j      # Neo4j

# Read-only enforcement
picklock --read-only postgres://postgres:postgres@localhost:5432/test
picklock --read-only redis://localhost:6379/0
```

---

## 1. Quick Test with SQLite (No Server Needed)

### 1a. Create a test database

```bash
go run ./cmd/demo
# creates demo.db with 20 tables
./picklock demo.db
```

### 1b. Navigation Basics

- `Tab` / `Alt+1`, `Alt+2`, `Alt+3`: Switch focus between Sidebar (catalog), Main View, and Query Editor.
- `↑↓` / `j k`: Navigate items/rows.
- `Enter` in sidebar: Open selected table/collection/key/label and focus main panel.
- `i`: Structure inspector (columns, collection stats, key metrics, clustering keys, graph schema).
- `v`: Detail view (full un-truncated row, formatted JSON document, KV entries, graph node & incident edges).
- `r`: Reload current item.
- `Alt+E`: Export query result or browsed page to `.csv` or `.json`.
- `Alt+C` / `Ctrl+Y`: Copy editor query to clipboard.
- `Ctrl+R`: Execute query under cursor in editor (separate multiple queries with `;` in SQL/CQL/Cypher, or newlines in Redis).
- `Esc`: Cancel running query or close detail/structure overlay.

---

## 2. Relational Engines (PostgreSQL / MySQL / MariaDB / SQL Server)

Start containers:

```bash
docker compose up -d postgres mysql mariadb mssql
make demo-pg demo-mysql demo-maria demo-mssql
```

Connect in `picklock`:
- **PostgreSQL**: engine `postgres`, host `localhost`, port `5432`, user `postgres`, pass `postgres`, db `test`
- **MySQL**: engine `mysql`, host `localhost`, port `3306`, user `root`, pass `root`, db `test`
- **MariaDB**: engine `mariadb`, host `localhost`, port `3307`, user `root`, pass `root`, db `test`
- **SQL Server**: engine `mssql`, host `localhost`, port `1433`, user `sa`, pass `Str0ng!Passw0rd`, db `master`

---

## 3. MongoDB (Document Database)

Start MongoDB:

```bash
docker compose up -d mongo
make demo-mongo
```

Connect:
- **Engine**: `mongo`
- **Host**: `localhost`, **Port**: `27017`, **Database**: `test`
- Or pass directly: `picklock mongodb://localhost:27017/test`

Features:
- **Sidebar**: Lists all collections in the database (`users`, `products`, `orders`).
- **Main View**: Displays documents with formatted `[_id] { summary }`.
- **Detail View (`v`)**: Pretty-printed indented BSON/JSON document.
- **Structure View (`i`)**: Collection document count, storage size, average document size, index sizes, and inferred field types.
- **Query Editor (`Ctrl+R`)**:
  - `db.users.find({ age: { $gt: 25 } })`
  - `db.users.countDocuments({})`
  - `db.users.insertOne({ name: "Charlie", role: "admin" })`
  - `db.users.deleteOne({ name: "Charlie" })`
  - Raw JSON commands: `{"find": "users", "limit": 5}`

---

## 4. Redis (Key-Value Data Store)

Start Redis:

```bash
docker compose up -d redis
make demo-redis
```

Connect:
- **Engine**: `redis`
- **Host**: `localhost`, **Port**: `6379`, **Database**: `0`
- Or pass directly: `picklock redis://localhost:6379/0`

Features:
- **Sidebar**: Non-blocking `SCAN` of keys with type badges (`[string]`, `[hash]`, `[list]`, `[set]`, `[zset]`).
- **Main View**:
  - `string`: full string value.
  - `hash`: table of fields and values.
  - `list`: indexed elements.
  - `set`: set members.
  - `zset`: members with scores.
- **Structure View (`i`)**: Key type, TTL, memory usage, encoding, element length, and server statistics.
- **Query Editor (`Ctrl+R`)**:
  - `GET app:config:title`
  - `HGETALL user:1001`
  - `LRANGE queue:tasks 0 10`
  - `SMEMBERS tags:popular`
  - `ZRANGE leaderboard:points 0 -1 WITHSCORES`
  - `SET mykey "Hello World"`

---

## 5. Apache Cassandra / ScyllaDB (Wide-Column Store)

Start Cassandra:

```bash
docker compose up -d cassandra
# Wait for Cassandra to be healthy (~30s): docker compose ps cassandra
make demo-cassandra
```

Connect:
- **Engine**: `cassandra`
- **Host**: `localhost`, **Port**: `9042`, **Database** (Keyspace): `picklock_demo`
- Or pass directly: `picklock cassandra://localhost:9042/picklock_demo`

Features:
- **Sidebar**: Tables in the keyspace (`users_by_country`, `sensor_readings`, `store_orders`).
- **Main View**: Tabular data with native CQL `PageState` cursor navigation (`PgDn`/`PgUp`).
- **Structure View (`i`)**: Clearly separates **Partition Keys (PK)**, **Clustering Columns (CC)**, and regular columns.
- **Query Editor (`Ctrl+R`)**:
  - `SELECT * FROM users_by_country WHERE country = 'US';`
  - `SELECT * FROM sensor_readings WHERE sensor_id = 'SEN-NORTH-01' LIMIT 10;`
  - `INSERT INTO users_by_country (country, user_id, name) VALUES ('CA', 9999, 'Dave');`

---

## 6. Neo4j (Graph Database)

Start Neo4j:

```bash
docker compose up -d neo4j
# Wait for Neo4j to be healthy (~15s): docker compose ps neo4j
make demo-neo4j
```

Connect:
- **Engine**: `neo4j`
- **Host**: `localhost`, **Port**: `7687`, **User**: `neo4j`, **Password**: `password`, **Database**: `neo4j`
- Or pass directly: `picklock neo4j://neo4j:password@localhost:7687/neo4j`

Features:
- **Sidebar**: Node labels (`Person`, `Movie`, `Company`).
- **Main View**: Nodes with label badges, properties preview, and incident edge counters.
- **Detail View (`v`)**: Node properties + expanded list of incident relationships with direction, type, and target summary:
  - `-> ACTED_IN (:Movie)`
  - `-> DIRECTED (:Movie)`
  - `-> WORKS_AT (:Company)`
- **Structure View (`i`)**: Label schema, inferred property types, indexes, and attached relationship types.
- **Query Editor (`Ctrl+R`)**:
  - `MATCH (p:Person)-[:ACTED_IN]->(m:Movie) RETURN p.name, m.title LIMIT 20;`
  - `MATCH (c:Company)<-[:WORKS_AT]-(p:Person) RETURN c.name, count(p);`
  - `CREATE (p:Person {name: 'Grace Hopper', born: 1906});`

---

## 7. Global Demo Seeding

To seed all 9 database engines at once with sample datasets:

```bash
docker compose up -d --wait
make demo-all
```
