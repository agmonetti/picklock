package conn

import "testing"

func TestParseDSN_SQLite(t *testing.T) {
	cases := map[string]string{
		"./app.db":       "./app.db",
		"/data/app.db":   "/data/app.db",
		"picklock.db":        "picklock.db",
		"sqlite:picklock.db": "picklock.db",
		"sqlite:/a/b.db": "/a/b.db",
		"file:/a/b.db":   "/a/b.db",
	}
	for dsn, want := range cases {
		cfg, err := ParseDSN(dsn)
		if err != nil {
			t.Fatalf("ParseDSN(%q): %v", dsn, err)
		}
		if cfg.Driver != DriverSQLite {
			t.Errorf("ParseDSN(%q) Driver = %s, want sqlite", dsn, cfg.Driver)
		}
		if cfg.Path != want {
			t.Errorf("ParseDSN(%q) Path = %q, want %q", dsn, cfg.Path, want)
		}
	}
}

func TestParseDSN_Postgres(t *testing.T) {
	cfg, err := ParseDSN("postgres://alice:secret@db.example.com:5444/myapp?sslmode=require")
	if err != nil {
		t.Fatalf("ParseDSN: %v", err)
	}
	if cfg.Driver != DriverPostgres || cfg.User != "alice" || cfg.Password != "secret" ||
		cfg.Host != "db.example.com" || cfg.Port != 5444 || cfg.Database != "myapp" ||
		cfg.SSLMode != "require" {
		t.Errorf("cfg = %+v", cfg)
	}
}

func TestParseDSN_Defaults(t *testing.T) {
	cfg, err := ParseDSN("postgres://host/db")
	if err != nil {
		t.Fatalf("ParseDSN: %v", err)
	}
	if cfg.Host != "host" || cfg.Port != 5432 || cfg.Database != "db" {
		t.Errorf("cfg = %+v, want host/localhost default port 5432", cfg)
	}
	if cfg.SSLMode != "" {
		t.Errorf("SSLModes = %q, want empty (engine default)", cfg.SSLMode)
	}
}

func TestParseDSN_MySQLMaria(t *testing.T) {
	for _, scheme := range []string{"mysql", "mariadb"} {
		cfg, err := ParseDSN(scheme + "://root@h:3307/test?tls=prefer")
		if err != nil {
			t.Fatalf("ParseDSN(%s): %v", scheme, err)
		}
		if cfg.Driver != Driver(scheme) || cfg.User != "root" || cfg.Port != 3307 ||
			cfg.Database != "test" || cfg.SSLMode != "prefer" {
			t.Errorf("%s: cfg = %+v", scheme, cfg)
		}
	}
}

func TestParseDSN_MSSQL(t *testing.T) {
	cfg, err := ParseDSN("sqlserver://sa:pw@h:1433?database=master&tls=disable")
	if err != nil {
		t.Fatalf("ParseDSN: %v", err)
	}
	if cfg.Driver != DriverMSSQL || cfg.User != "sa" || cfg.Password != "pw" ||
		cfg.Database != "master" || cfg.SSLMode != "disable" {
		t.Errorf("cfg = %+v", cfg)
	}
}

func TestParseDSN_NonRelational(t *testing.T) {
	// MongoDB
	mongoCfg, err := ParseDSN("mongodb://user:pass@mongo.local:27018/mydb?sslmode=require")
	if err != nil {
		t.Fatalf("ParseDSN(mongo): %v", err)
	}
	if mongoCfg.Driver != DriverMongo || mongoCfg.Port != 27018 || mongoCfg.Database != "mydb" {
		t.Errorf("mongoCfg = %+v", mongoCfg)
	}

	// Redis
	redisCfg, err := ParseDSN("redis://:pass@redis.local:6380/2")
	if err != nil {
		t.Fatalf("ParseDSN(redis): %v", err)
	}
	if redisCfg.Driver != DriverRedis || redisCfg.Port != 6380 || redisCfg.Database != "2" {
		t.Errorf("redisCfg = %+v", redisCfg)
	}

	// Cassandra
	cassCfg, err := ParseDSN("cassandra://cass.local:9042/my_keyspace")
	if err != nil {
		t.Fatalf("ParseDSN(cassandra): %v", err)
	}
	if cassCfg.Driver != DriverCassandra || cassCfg.Port != 9042 || cassCfg.Database != "my_keyspace" {
		t.Errorf("cassCfg = %+v", cassCfg)
	}

	// Neo4j
	neoCfg, err := ParseDSN("neo4j://neo4j:secret@neo.local:7687/neo4j")
	if err != nil {
		t.Fatalf("ParseDSN(neo4j): %v", err)
	}
	if neoCfg.Driver != DriverNeo4j || neoCfg.Port != 7687 || neoCfg.Database != "neo4j" {
		t.Errorf("neoCfg = %+v", neoCfg)
	}
}

func TestParseDSN_Errors(t *testing.T) {
	bad := []string{
		"",
		"oracle://h/db",               // unsupported scheme
		"postgres://host:notaport/db", // bad port
		"postgres://host:99999/db",    // port out of range
	}
	for _, dsn := range bad {
		if _, err := ParseDSN(dsn); err == nil {
			t.Errorf("ParseDSN(%q) expected an error", dsn)
		}
	}
}
