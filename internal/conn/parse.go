package conn

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// ParseDSN converts a command-line argument or URI into a ConnectionConfig:
//
//	picklock ./app.db                             SQLite (a bare path, absolute or relative)
//	picklock sqlite:/abs/app.db                   SQLite via URI
//	picklock postgres://u:p@host:5432/db          PostgreSQL (also mysql://, mariadb://)
//	picklock sqlserver://u:p@host:1433?database=db
//	picklock mongodb://u:p@host:27017/db          MongoDB (also mongodb+srv://)
//	picklock redis://:pass@host:6379/0            Redis (also rediss://)
//	picklock cassandra://host:9042/keyspace       Cassandra (also cql://)
//	picklock neo4j://u:p@host:7687/neo4j          Neo4j (also bolt://, neo4j+s://)
//
// Any engine accepts ?sslmode= or ?tls= in the URL query, which maps to
// ConnectionConfig.SSLMode. Missing host/port fall back to localhost and the
// engine default port.
func ParseDSN(dsn string) (ConnectionConfig, error) {
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		return ConnectionConfig{}, fmt.Errorf("empty DSN")
	}

	u, err := url.Parse(dsn)
	if err != nil {
		return ConnectionConfig{}, fmt.Errorf("invalid DSN %q: %v", redactURI(dsn), err)
	}

	var cfg ConnectionConfig
	q := u.Query()
	scheme := strings.ToLower(u.Scheme)

	switch scheme {
	case "sqlite", "file":
		cfg.Driver = DriverSQLite
		cfg.Path = u.Path
		if cfg.Path == "" {
			cfg.Path = u.Opaque // e.g. "sqlite:picklock.db", "file:relm.db"
		}
		if u.Host != "" {
			return ConnectionConfig{}, fmt.Errorf("invalid DSN %q: sqlite path must not have a host", redactURI(dsn))
		}
		return cfg, nil

	case "postgres", "postgresql":
		cfg.Driver = DriverPostgres
	case "mysql":
		cfg.Driver = DriverMySQL
	case "mariadb":
		cfg.Driver = DriverMariaDB
	case "sqlserver", "mssql":
		cfg.Driver = DriverMSSQL
	case "mongodb", "mongodb+srv":
		cfg.Driver = DriverMongo
		cfg.URI = dsn
	case "redis", "rediss":
		cfg.Driver = DriverRedis
		cfg.URI = dsn
		if scheme == "rediss" {
			cfg.SSLMode = "require"
		}
	case "cassandra", "cql":
		cfg.Driver = DriverCassandra
	case "neo4j", "bolt", "neo4j+s", "neo4j+ssc":
		cfg.Driver = DriverNeo4j
		cfg.URI = dsn

	case "":
		// no scheme → a plain SQLite file path
		return ConnectionConfig{Driver: DriverSQLite, Path: dsn}, nil

	default:
		return ConnectionConfig{}, fmt.Errorf("unsupported DSN scheme %q", u.Scheme)
	}

	if u.User != nil {
		cfg.User = u.User.Username()
		cfg.Password, _ = u.User.Password()
	}
	cfg.Host = u.Hostname()
	if cfg.Host == "" {
		cfg.Host = "localhost"
	}
	cfg.Port = DefaultPort(cfg.Driver)
	if p := u.Port(); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n <= 0 || n > 65535 {
			return ConnectionConfig{}, fmt.Errorf("invalid DSN %q: bad port %q", redactURI(dsn), p)
		}
		cfg.Port = n
	}

	if cfg.Driver == DriverMSSQL {
		cfg.Database = q.Get("database")
		if cfg.Database == "" {
			cfg.Database = q.Get("Database")
		}
		if cfg.Database == "" {
			cfg.Database = q.Get("initial catalog")
		}
		if cfg.Database == "" {
			cfg.Database = q.Get("Initial Catalog")
		}
		if cfg.Database == "" && u.Path != "" {
			cfg.Database = strings.TrimPrefix(u.Path, "/")
			if decoded, err := url.PathUnescape(cfg.Database); err == nil {
				cfg.Database = decoded
			}
		}
	} else if u.Path != "" {
		cfg.Database = strings.TrimPrefix(u.Path, "/")
		if decoded, err := url.PathUnescape(cfg.Database); err == nil {
			cfg.Database = decoded
		}
	}

	if cfg.Driver == DriverRedis && cfg.Database == "" {
		cfg.Database = "0"
	}

	if v := q.Get("sslmode"); v != "" {
		cfg.SSLMode = v
	} else if v := q.Get("tls"); v != "" {
		cfg.SSLMode = v
	}
	return cfg, nil
}
