# 00 — Research: Architecture Audit & Non-Relational Paradigms

## 1. Executive Summary

`picklock` was initially designed around a focused premise: a terminal browser for relational databases (SQLite, PostgreSQL, MySQL, MariaDB, SQL Server).

This research document audits every relational assumption in `picklock`'s initial architecture and evaluates non-relational database paradigms (Document, Key-Value / Data Structures, Wide-Column, Graph, and Search/Multi-model) to determine how `picklock` can evolve into an extensible **terminal data browser** without forcing non-relational engines into relational concepts.

---

## 2. Audit of Existing Relational Assumptions

An exhaustive audit of the initial codebase revealed relational assumptions embedded across multiple layers:

| Layer / Subsystem | Assumption in Codebase | Multi-Paradigm Reality |
|---|---|---|
| **Data Hierarchy** | `Database → Tables → Columns × Rows` | **Document (Mongo)**: `Database → Collections → Documents`<br>**Key-Value (Redis)**: `Database (0..15) → Keys → Strings/Hashes/Lists/Sets/ZSets`<br>**Wide-Column (Cassandra)**: `Keyspace → Tables → Partitions & Clustering Rows`<br>**Graph (Neo4j)**: `Database → Node Labels & Rel Types → Nodes, Properties & Edges` |
| **Catalog Navigation** | `Store.Tables() []string` and Sidebar title `"TABLES"` | Navigation items are not always tables. In MongoDB they are collections; in Redis they are keys (or namespace prefixes); in Neo4j they are Node Labels and Relationship Types. |
| **Schema & Introspection** | `Store.Columns(table) []Column` with Type, PK, NotNull, Default | MongoDB documents have dynamic, hierarchical schemas. Redis keys have types, TTLs, and encodings, not columns. Neo4j has node labels, property keys, and edge definitions. Cassandra has Partition Keys (`PK`) and Clustering Columns (`CC`). |
| **Result Representation** | `Result{Columns []string, Rows [][]string, Nulls [][]bool}` | Rectangular row matrices cannot represent nested JSON documents, typed Redis structures (hashes/lists/sets/zsets), or graph nodes with incident edges without loss of structure. |
| **Pagination** | Keyset (`WHERE pk > ? ORDER BY pk`) or `LIMIT n OFFSET m` | MongoDB paginates via cursor or ObjectId; Redis iterates keys via `SCAN` and slices collections via `LRANGE`/`ZRANGE`; Cassandra paginates via driver paging states; Neo4j paginates via Cypher `SKIP/LIMIT`. |
| **Query Engine** | SQL string with `;` splitting and keyword detection (`SELECT`, `INSERT`, etc.) | MongoDB uses JSON filter/pipeline syntax; Redis uses whitespace-delimited commands (`GET`, `HGETALL`, `INFO`); Cassandra uses CQL; Neo4j uses Cypher. |
| **Connection Model** | `Host`, `Port`, `User`, `Password`, `Database`, `SSL` or SQLite `Path` | MongoDB and Neo4j frequently use URIs (`mongodb+srv://`, `bolt://`, `neo4j://`); Redis identifies databases by numeric index (0..15); Cassandra identifies namespaces as keyspaces. |
| **Mutation Detection & Reload** | `browser.Reload()` runs `COUNT(*)` after write queries | A write in Redis (`SET`/`DEL`) requires re-scanning keys; in MongoDB (`insert`/`drop`) it requires re-listing collections; in Cassandra it requires CQL partition querying. Running `COUNT(*)` on non-relational stores is either invalid or prohibitively expensive. |

---

## 3. Database Paradigm Evaluation

We investigated candidates across diverse database paradigms against key architectural criteria:
1. **Popularity & Usefulness**
2. **Go Ecosystem Maturity** (Pure Go, license, maintenance)
3. **Terminal Exploration Suitability** (Can data be inspected interactively from keyboard?)
4. **Conceptual Diversity** (Does it challenge and improve `picklock`'s architecture?)
5. **Local Development & CI Feasibility** (Can it be tested deterministically in Docker?)

### Candidate Evaluation Matrix

| Engine | Paradigm | Primary Object | Query Language / API | Natural Browser Model | Go Driver Status | Inclusion Decision |
|---|---|---|---|---|---|---|
| **MongoDB** | Document | Collection | JSON / Mongo Query Language | Databases → Collections → Documents | `go.mongodb.org/mongo-driver/v2` (Pure Go, Apache 2.0) | **Included (Stage 1A)** |
| **Redis** | Key-Value / In-Memory Structures | Key | Redis Commands (`GET`, `HGETALL`, `SCAN`) | DBs (0..15) → Keys → Typed Values | `github.com/redis/go-redis/v9` (Pure Go, BSD-2) | **Included (Stage 1B)** |
| **Cassandra** | Wide-Column | Table | CQL | Keyspaces → Tables → Partitions & Rows | `github.com/gocql/gocql` (Pure Go, BSD-3) | **Included (Stage 2)** |
| **Neo4j** | Graph | Node / Relationship | Cypher | Databases → Node Labels & Rel Types → Graph | `github.com/neo4j/neo4j-go-driver/v5` (Pure Go, Apache 2.0) | **Included (Stage 3)** |
| **Elasticsearch / OpenSearch** | Search / Document | Index | Query DSL / ES\|QL | Clusters → Indices → Documents | `elastic/go-elasticsearch` (Apache 2.0) | Evaluated (Future) |
| **DynamoDB** | Key-Value / Document | Table | PartiQL / AWS SDK | Regions → Tables → Items | `aws/aws-sdk-go-v2` (Apache 2.0) | Evaluated (Future) |
| **ClickHouse** | Column-Oriented OLAP | Table | SQL | Databases → Tables → Rows | `ClickHouse/clickhouse-go` (Apache 2.0) | Evaluated (Future) |

---

## 4. In-Depth Analysis of Core Candidates

### 1. MongoDB (Document Paradigm)
- **Data Model**: Collections of heterogeneous BSON documents with arbitrary nesting, arrays, and sub-objects.
- **Natural UX**:
  - Sidebar: List of collections with estimated document count.
  - Main View: Compact list of documents showing `_id` and top-level summary preview.
  - Detail View (`v`): Formatted, indented, syntax-highlightable JSON document.
  - Structure View (`i`): Collection statistics (count, storage size, avg object size, indexes).
  - Query: JSON query filters (e.g. `{"status": "active"}`) or aggregation pipelines.

### 2. Redis (Key-Value & Data Structures Paradigm)
- **Data Model**: In-memory database indexed by keys containing strings, hashes, lists, sets, sorted sets, or streams.
- **Natural UX**:
  - Sidebar: Key list discovered safely via cursor `SCAN` (never blocking `KEYS *`).
  - Main View: Slices the active key's value based on its type:
    - String: Key & Value
    - Hash: Field & Value table
    - List: Index & Element table
    - Set: Member table
    - Sorted Set: Rank, Member, and Score table
  - Structure View (`i`): Key metadata (Type, TTL, Memory Usage via `MEMORY USAGE`, Encoding, Length).
  - Query: Redis command execution (`GET`, `HGETALL`, `LRANGE`, `INFO`, `SCAN`, `DBSIZE`).

### 3. Apache Cassandra (Wide-Column Paradigm)
- **Data Model**: Keyspaces containing tables partitioned by Partition Keys and sorted within partitions by Clustering Columns.
- **Natural UX**:
  - Sidebar: List of tables within the selected keyspace.
  - Main View: Tabular CQL rows with keyset / paging state navigation.
  - Structure View (`i`): Explicit distinction of Partition Keys (`PK`), Clustering Columns (`CC`), and CQL data types (`uuid`, `text`, `map`, `list`, `set`).
  - Query: CQL query execution (`SELECT * FROM table WHERE pk = ?`).

### 4. Neo4j (Graph Paradigm)
- **Data Model**: Labeled property graph containing Nodes (with labels and key-value properties) and directed Relationships (with types and properties).
- **Natural UX**:
  - Sidebar: List of Node Labels (e.g. `User`, `Product`) and Relationship Types (e.g. `PURCHASED`, `OWNS`).
  - Main View: Node summary list with primary labels and key properties.
  - Detail View (`v`): Node property inspector with interactive list of incoming and outgoing incident relationships.
  - Structure View (`i`): Label schema (properties, types, unique constraints, and indexes).
  - Query: Cypher query execution (`MATCH (n:User) RETURN n LIMIT 25`).

---

## 5. Architectural Conclusions

1. **Abandon Monolithic `Store`**: An engine must not implement fake methods. We replace the relational `Store` with a composable `DataSource` contract.
2. **Abandon Flat `Result`**: We introduce semantic, self-describing `DataView` types (`TabularData`, `DocumentData`, `KeyValueData`, `GraphData`, `RawTextData`).
3. **Preserve Relational Engines**: SQLite, PostgreSQL, MySQL, MariaDB, and SQL Server are adapted through a shared `RelationalAdapter` without rewriting their dialects.
4. **Isolate Engine Concerns**: The TUI dispatches on self-describing views rather than scattered `switch driver` conditionals.
