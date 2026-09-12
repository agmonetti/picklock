// Command demo creates example datasets to try picklock without manual setup:
//
//	go run ./cmd/demo                 # SQLite demo.db (no server needed)
//	go run ./cmd/demo --all           # every engine (relational + non-relational)
//	go run ./cmd/demo --mongo         # MongoDB collections (users, products, orders)
//	go run ./cmd/demo --redis         # Redis keys (strings, hashes, lists, sets, zsets)
//	go run ./cmd/demo --cassandra     # Cassandra keyspace (picklock_demo)
//	go run ./cmd/demo --neo4j         # Neo4j graph nodes & relationships
//	go run ./cmd/demo --postgres      # PostgreSQL
//
// For network engines, start the containers first:
//
//	docker compose up -d
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/gocql/gocql"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "github.com/microsoft/go-mssqldb"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
	goredis "github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
	_ "modernc.org/sqlite"

	"github.com/agmonetti/picklock/internal/demo"
)

var defaults = map[string]demo.Config{
	"postgres":  {Host: "localhost", Port: 5432, User: "postgres", Password: "postgres", Database: "test"},
	"mysql":     {Host: "localhost", Port: 3306, User: "root", Password: "root", Database: "test"},
	"mariadb":   {Host: "localhost", Port: 3307, User: "root", Password: "root", Database: "test"},
	"mssql":     {Host: "localhost", Port: 1433, User: "sa", Password: "Str0ng!Passw0rd", Database: "master"},
	"mongo":     {Host: "localhost", Port: 27017, Database: "test"},
	"redis":     {Host: "localhost", Port: 6379, Database: "0"},
	"cassandra": {Host: "localhost", Port: 9042, Database: "picklock_demo"},
	"neo4j":     {Host: "localhost", Port: 7687, User: "neo4j", Password: "password", Database: "neo4j"},
}

var allEngines = []string{
	"sqlite", "postgres", "mysql", "mariadb", "mssql",
	"mongo", "redis", "cassandra", "neo4j",
}

func main() {
	var sqlite, postgres, mysql, mariadb, mssql bool
	var mongoFlag, redisFlag, cassandraFlag, neo4jFlag bool
	var all bool

	flag.BoolVar(&all, "all", false, "seed every engine")
	flag.BoolVar(&sqlite, "sqlite", false, "seed SQLite (demo.db)")
	flag.BoolVar(&postgres, "postgres", false, "seed PostgreSQL")
	flag.BoolVar(&mysql, "mysql", false, "seed MySQL")
	flag.BoolVar(&mariadb, "mariadb", false, "seed MariaDB")
	flag.BoolVar(&mssql, "mssql", false, "seed SQL Server")
	flag.BoolVar(&mongoFlag, "mongo", false, "seed MongoDB")
	flag.BoolVar(&redisFlag, "redis", false, "seed Redis")
	flag.BoolVar(&cassandraFlag, "cassandra", false, "seed Cassandra")
	flag.BoolVar(&neo4jFlag, "neo4j", false, "seed Neo4j")
	flag.Parse()

	engines := map[string]bool{
		"sqlite": sqlite, "postgres": postgres, "mysql": mysql,
		"mariadb": mariadb, "mssql": mssql,
		"mongo": mongoFlag, "redis": redisFlag, "cassandra": cassandraFlag, "neo4j": neo4jFlag,
	}
	if all {
		for _, e := range allEngines {
			engines[e] = true
		}
	}
	if !any(engines) {
		engines["sqlite"] = true
	}

	fail := 0
	for _, e := range allEngines {
		if !engines[e] {
			continue
		}
		if err := seedEngine(e); err != nil {
			fmt.Fprintf(os.Stderr, "demo %-10s: %v\n", e, err)
			fail++
			continue
		}
		fmt.Printf("Example database created: %s\n", hint(e))
	}
	if fail > 0 {
		os.Exit(1)
	}
}

func any(m map[string]bool) bool {
	for _, v := range m {
		if v {
			return true
		}
	}
	return false
}

func hint(e string) string {
	switch e {
	case "sqlite":
		return "open it with picklock (engine SQLite, path demo.db)"
	case "postgres":
		return "open it with picklock (PostgreSQL, localhost:5432, user postgres, database test)"
	case "mysql":
		return "open it with picklock (MySQL, localhost:3306, user root, database test)"
	case "mariadb":
		return "open it with picklock (MariaDB, localhost:3307, user root, database test)"
	case "mssql":
		return "open it with picklock (SQL Server, localhost:1433, user sa, database master)"
	case "mongo":
		return "open it with picklock (MongoDB, localhost:27017, database test)"
	case "redis":
		return "open it with picklock (Redis, localhost:6379, db 0)"
	case "cassandra":
		return "open it with picklock (Cassandra, localhost:9042, keyspace picklock_demo)"
	case "neo4j":
		return "open it with picklock (Neo4j, localhost:7687, user neo4j, password password)"
	}
	return ""
}

func seedEngine(engine string) error {
	switch engine {
	case "mongo":
		return seedMongo()
	case "redis":
		return seedRedis()
	case "cassandra":
		return seedCassandra()
	case "neo4j":
		return seedNeo4j()
	default:
		return seedRelational(engine)
	}
}

func seedMongo() error {
	cfg := cfgFor("mongo")
	uri := fmt.Sprintf("mongodb://%s:%d/%s", cfg.Host, cfg.Port, cfg.Database)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		return fmt.Errorf("mongo connect: %w", err)
	}
	defer client.Disconnect(ctx)

	if err := client.Ping(ctx, readpref.Primary()); err != nil {
		return fmt.Errorf("cannot reach MongoDB (%s): %w", uri, err)
	}
	start := time.Now()
	if err := demo.SeedMongo(ctx, client, cfg.Database); err != nil {
		return err
	}
	fmt.Printf("  mongo: 3 collections seeded in %s\n", time.Since(start).Round(time.Millisecond))
	return nil
}

func seedRedis() error {
	cfg := cfgFor("redis")
	dbIdx, _ := strconv.Atoi(cfg.Database)
	client := goredis.NewClient(&goredis.Options{
		Addr: fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		DB:   dbIdx,
	})
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("cannot reach Redis (localhost:%d): %w", cfg.Port, err)
	}
	start := time.Now()
	if err := demo.SeedRedis(ctx, client); err != nil {
		return err
	}
	fmt.Printf("  redis: keys (strings, hashes, lists, sets, zsets) seeded in %s\n", time.Since(start).Round(time.Millisecond))
	return nil
}

func seedCassandra() error {
	cfg := cfgFor("cassandra")
	cluster := gocql.NewCluster(cfg.Host)
	cluster.Port = cfg.Port
	cluster.ConnectTimeout = 10 * time.Second
	cluster.Timeout = 10 * time.Second

	session, err := cluster.CreateSession()
	if err != nil {
		return fmt.Errorf("cannot reach Cassandra (localhost:%d): %w", cfg.Port, err)
	}
	defer session.Close()

	start := time.Now()
	if err := demo.SeedCassandra(session, cfg.Database); err != nil {
		return err
	}
	fmt.Printf("  cassandra: keyspace %s & 3 tables seeded in %s\n", cfg.Database, time.Since(start).Round(time.Millisecond))
	return nil
}

func seedNeo4j() error {
	cfg := cfgFor("neo4j")
	uri := fmt.Sprintf("neo4j://%s:%d", cfg.Host, cfg.Port)
	auth := neo4jdriver.BasicAuth(cfg.User, cfg.Password, "")

	driver, err := neo4jdriver.NewDriverWithContext(uri, auth)
	if err != nil {
		return fmt.Errorf("neo4j driver: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	defer driver.Close(ctx)

	if err := driver.VerifyConnectivity(ctx); err != nil {
		return fmt.Errorf("cannot reach Neo4j (%s): %w", uri, err)
	}
	start := time.Now()
	if err := demo.SeedNeo4j(ctx, driver, cfg.Database); err != nil {
		return err
	}
	fmt.Printf("  neo4j: nodes & relationships seeded in %s\n", time.Since(start).Round(time.Millisecond))
	return nil
}

func seedRelational(engine string) error {
	cfg := cfgFor(engine)
	if engine == "sqlite" {
		path := cfg.Path
		if path == "" {
			path = "demo.db"
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	db, err := sql.Open(demo.DriverName(engine), demo.DSN(engine, cfg))
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("cannot reach the server (%s): %w", demo.DSN(engine, redact(cfg)), err)
	}

	start := time.Now()
	if err := demo.Seed(db, engine); err != nil {
		return err
	}
	fmt.Printf("  %s: 20 tables seeded in %s\n", engine, time.Since(start).Round(time.Millisecond))
	return nil
}

func cfgFor(engine string) demo.Config {
	cfg := defaults[engine]
	getenv := func(name, def string) string {
		if v := os.Getenv(name); v != "" {
			return v
		}
		return def
	}

	switch engine {
	case "sqlite":
		path := "demo.db"
		if len(os.Args) > 1 && !isFlag(os.Args[1]) {
			path = os.Args[1]
		}
		if p := os.Getenv("DEMO_SQLITE_PATH"); p != "" {
			path = p
		}
		return demo.Config{Path: path}
	case "postgres":
		cfg.Host = getenv("POSTGRES_HOST", cfg.Host)
		cfg.Port = intEnv("POSTGRES_PORT", cfg.Port)
		cfg.User = getenv("POSTGRES_USER", cfg.User)
		cfg.Password = getenv("POSTGRES_PASSWORD", cfg.Password)
		cfg.Database = getenv("POSTGRES_DATABASE", cfg.Database)
	case "mysql":
		cfg.Host = getenv("MYSQL_HOST", cfg.Host)
		cfg.Port = intEnv("MYSQL_PORT", cfg.Port)
		cfg.User = getenv("MYSQL_USER", cfg.User)
		cfg.Password = getenv("MYSQL_PASSWORD", cfg.Password)
		cfg.Database = getenv("MYSQL_DATABASE", cfg.Database)
	case "mariadb":
		cfg.Host = getenv("MARIADB_HOST", cfg.Host)
		cfg.Port = intEnv("MARIADB_PORT", cfg.Port)
		cfg.User = getenv("MARIADB_USER", cfg.User)
		cfg.Password = getenv("MARIADB_PASSWORD", cfg.Password)
		cfg.Database = getenv("MARIADB_DATABASE", cfg.Database)
	case "mssql":
		cfg.Host = getenv("MSSQL_HOST", cfg.Host)
		cfg.Port = intEnv("MSSQL_PORT", cfg.Port)
		cfg.User = getenv("MSSQL_USER", cfg.User)
		cfg.Password = getenv("MSSQL_PASSWORD", cfg.Password)
		cfg.Database = getenv("MSSQL_DATABASE", cfg.Database)
	case "mongo":
		cfg.Host = getenv("MONGO_HOST", cfg.Host)
		cfg.Port = intEnv("MONGO_PORT", cfg.Port)
		cfg.Database = getenv("MONGO_DATABASE", cfg.Database)
	case "redis":
		cfg.Host = getenv("REDIS_HOST", cfg.Host)
		cfg.Port = intEnv("REDIS_PORT", cfg.Port)
		cfg.Database = getenv("REDIS_DATABASE", cfg.Database)
	case "cassandra":
		cfg.Host = getenv("CASSANDRA_HOST", cfg.Host)
		cfg.Port = intEnv("CASSANDRA_PORT", cfg.Port)
		cfg.Database = getenv("CASSANDRA_KEYSPACE", cfg.Database)
	case "neo4j":
		cfg.Host = getenv("NEO4J_HOST", cfg.Host)
		cfg.Port = intEnv("NEO4J_PORT", cfg.Port)
		cfg.User = getenv("NEO4J_USER", cfg.User)
		cfg.Password = getenv("NEO4J_PASSWORD", cfg.Password)
		cfg.Database = getenv("NEO4J_DATABASE", cfg.Database)
	}
	return cfg
}

func intEnv(name string, def int) int {
	if v := os.Getenv(name); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func isFlag(s string) bool {
	return len(s) > 1 && s[0] == '-'
}

func redact(cfg demo.Config) demo.Config {
	cfg.Password = "xxxx"
	return cfg
}
