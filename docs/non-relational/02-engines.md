# 02 — Engines: Supported Systems & Rationale

## 1. Engine Directory

`picklock` supports nine engines across four distinct database paradigms:

| Engine | Paradigm | Driver | License | Default Port | Default Query Language |
|---|---|---|---|---|---|
| **SQLite** | Relational | `modernc.org/sqlite` | BSD-3 (Pure Go) | N/A (File) | SQL |
| **PostgreSQL** | Relational | `github.com/jackc/pgx/v5` | MIT (Pure Go) | 5432 | SQL |
| **MySQL** | Relational | `github.com/go-sql-driver/mysql` | MPL-2.0 (Pure Go) | 3306 | SQL |
| **MariaDB** | Relational | `github.com/go-sql-driver/mysql` | MPL-2.0 (Pure Go) | 3306 (3307 compose) | SQL |
| **SQL Server** | Relational | `github.com/microsoft/go-mssqldb` | MIT (Pure Go) | 1433 | T-SQL |
| **MongoDB** | Document | `go.mongodb.org/mongo-driver` v1.17 | Apache 2.0 (Pure Go) | 27017 | MQL / JSON |
| **Redis** | Key-Value / Structures | `github.com/redis/go-redis/v9` | BSD-2 (Pure Go) | 6379 | Redis Commands |
| **Apache Cassandra** | Wide-Column | `github.com/gocql/gocql` | BSD-3 (Pure Go) | 9042 | CQL |
| **Neo4j** | Graph | `github.com/neo4j/neo4j-go-driver/v5` | Apache 2.0 (Pure Go) | 7687 | Cypher |

---

## 2. In-Depth Engine Profiles

### 1. MongoDB (Document Paradigm)
- **Why Included**: MongoDB is the world's most widely used document database. It introduces hierarchical JSON/BSON structures that stress-test whether `picklock` can display nested data without flattening.
- **Go Ecosystem**: Official `go.mongodb.org/mongo-driver/v2/mongo` package provides idiomatic, high-performance, pure Go connectivity with full BSON codec support.
- **Catalog Model**: Lists collections in the target database (`Database.ListCollectionNames`).
- **Browsing Model**: Queries documents using `collection.Find()` with limit and cursor/skip pagination. Documents are serialized to `DocumentItem` with compact preview summaries and indented JSON.
- **Structure Model**: Inferred collection statistics (`collStats`), document counts, and index metadata.
- **Query Model**: Executes JSON filter objects (`{"status": "active"}`), aggregation pipelines (`[{"$match": ...}]`), or find wrappers (`find(...)`).

### 2. Redis (Key-Value & Data Structures Paradigm)
- **Why Included**: Redis represents key-value and in-memory data structures. It prevents `picklock` from assuming all data sources are table-oriented.
- **Go Ecosystem**: `github.com/redis/go-redis/v9` is the standard, battle-tested Go client.
- **Catalog Model**: Discovers keys safely with cursor iteration via `SCAN 0 COUNT 100` with optional key pattern filtering. **Never issues `KEYS *`**.
- **Browsing Model**: Inspects the active key's native data type via `TYPE` and fetches entries:
  - **String**: Key and string value.
  - **Hash**: Fields and values (`HSCAN` / `HGETALL`).
  - **List**: Indexed elements (`LRANGE 0 49`).
  - **Set**: Members (`SSCAN` / `SMEMBERS`).
  - **Sorted Set**: Members and score weights (`ZREVRANGE 0 49 WITHSCORES`).
- **Structure Model**: Key metadata: Type, TTL (via `TTL`), Memory Usage (via `MEMORY USAGE`), Encoding (via `OBJECT ENCODING`), and Length.
- **Query Model**: Redis command tokenizer executing commands (`GET`, `HGETALL`, `LRANGE`, `INFO`, `SCAN`, `DBSIZE`, `TTL`, `TYPE`, etc.).

### 3. Apache Cassandra (Wide-Column Paradigm)
- **Why Included**: Cassandra represents the wide-column distributed paradigm. While using tabular CQL syntax, its partition key and clustering column data model differs fundamentally from relational models.
- **Go Ecosystem**: `github.com/gocql/gocql` is the standard pure Go driver implementing the native CQL binary protocol.
- **Catalog Model**: Lists tables in the target keyspace via `system_schema.tables`.
- **Browsing Model**: Paginated CQL queries (`SELECT * FROM table LIMIT limit`) with driver paging state tokens.
- **Structure Model**: Introspects `system_schema.columns` to explicitly distinguish Partition Keys (`PK`), Clustering Columns (`CC`), and rich CQL types (`uuid`, `timestamp`, `list`, `map`, `set`).
- **Query Model**: Arbitrary CQL query execution (`SELECT`, `INSERT`, `UPDATE`).

### 4. Neo4j (Graph Paradigm)
- **Why Included**: Neo4j introduces the graph paradigm: nodes, labeled property graphs, and directed relationships.
- **Go Ecosystem**: `github.com/neo4j/neo4j-go-driver/v5` is the official, pure Go Bolt driver.
- **Catalog Model**: Discovers Node Labels (`SHOW LABELS`) and Relationship Types (`SHOW RELATIONSHIP TYPES`).
- **Browsing Model**: Queries nodes for the active label (`MATCH (n:Label) RETURN n SKIP s LIMIT l`), extracts node properties, and resolves incident incoming and outgoing relationships.
- **Structure Model**: Introspects label property types, unique constraints, and indexes.
- **Query Model**: Cypher query execution (`MATCH (u:User)-[r]->(p) RETURN u, r, p`).

---

## 3. Evaluation of Future Engines

| Candidate | Paradigm | Evaluation & Strategy |
|---|---|---|
| **Elasticsearch** | Search / Document | Can implement `DataSource` using `elastic/go-elasticsearch` and return `DocumentData` directly without core changes. |
| **DynamoDB** | Key-Value / Document | Can implement `DataSource` using AWS SDK v2, mapping table items to `DocumentData` or `TabularData`. |
| **ClickHouse** | Columnar OLAP | Can implement `DataSource` using `clickhouse-go` (wrapping `database/sql` via `RelationalAdapter`). |
