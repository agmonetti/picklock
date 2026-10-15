package store_test

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/agmonetti/picklock/internal/conn"
	"github.com/agmonetti/picklock/internal/store"
	"github.com/gocql/gocql"
	goredis "github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/bson"
	mongodriver "go.mongodb.org/mongo-driver/mongo"
	mongooptions "go.mongodb.org/mongo-driver/mongo/options"

	_ "github.com/agmonetti/picklock/internal/store/cassandra"
	_ "github.com/agmonetti/picklock/internal/store/mongo"
	_ "github.com/agmonetti/picklock/internal/store/mssql"
	_ "github.com/agmonetti/picklock/internal/store/mysql"
	_ "github.com/agmonetti/picklock/internal/store/neo4j"
	_ "github.com/agmonetti/picklock/internal/store/postgres"
	_ "github.com/agmonetti/picklock/internal/store/redis"
	_ "github.com/agmonetti/picklock/internal/store/sqlite"
)

const e6MeasureBytes = 1 << 20

// BenchmarkEditorDriverBudget exercises each registered executor against an
// oversized result. B/op and ns/op include the query, driver fetch/decode,
// bounded conversion, and the executor's cursor close where that engine has a
// cursor. They are end-to-end values, not isolated driver or close costs.
func BenchmarkEditorDriverBudget(b *testing.B) {
	for _, driver := range []conn.Driver{
		conn.DriverSQLite, conn.DriverPostgres, conn.DriverMySQL, conn.DriverMariaDB,
		conn.DriverMSSQL, conn.DriverMongo, conn.DriverRedis, conn.DriverCassandra,
		conn.DriverNeo4j,
	} {
		b.Run(string(driver), func(b *testing.B) {
			cfg, query, cleanup, configured := e6MeasurementSetup(b, driver)
			if !configured {
				b.Skip("driver service not configured")
			}
			if cleanup != nil {
				defer cleanup()
			}
			ds, err := store.New(cfg)
			if err != nil {
				b.Fatal(err)
			}
			defer ds.Close()

			limits := store.QueryLimits{MaxRows: 1, MaxCellBytes: 64 << 10, MaxResultBytes: 64 << 10}
			ctx := context.Background()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				data, err := ds.Query().Execute(ctx, query, 0, limits)
				if err != nil {
					b.Fatal(err)
				}
				status, ok := e6ResultStatus(data)
				wantBytes, wantItems := int64(64<<10), 1
				if driver == conn.DriverMongo {
					wantBytes, wantItems = 0, 0
				}
				if !ok || status.Reason == "" || status.RetainedBytes != wantBytes || status.RetainedItems != wantItems {
					b.Fatalf("%s result status = %+v, want cutoff with %d items/%d B", driver, status, wantItems, wantBytes)
				}
			}
			b.StopTimer()
		})
	}
}

func e6MeasurementSetup(b *testing.B, driver conn.Driver) (conn.ConnectionConfig, string, func(), bool) {
	b.Helper()
	payload := strings.Repeat("x", e6MeasureBytes)
	prefix := "PICKLOCK_TEST_" + strings.ToUpper(string(driver))
	cfg := conn.New(driver)
	cfg.Database = os.Getenv(prefix + "_DATABASE")
	if cfg.Database == "" {
		cfg.Database = "test"
		if driver == conn.DriverNeo4j {
			cfg.Database = "neo4j"
		}
	}
	cfg.Host = os.Getenv(prefix + "_HOST")
	if driver == conn.DriverSQLite {
		path := filepath.Join(b.TempDir(), "e6-measure.sqlite")
		file, err := os.Create(path)
		if err != nil {
			b.Fatal(err)
		}
		if err := file.Close(); err != nil {
			b.Fatal(err)
		}
		cfg.Path = path
		return cfg, "SELECT zeroblob(1048576)", nil, true
	}
	if cfg.Host == "" {
		return cfg, "", nil, false
	}
	cfg.Port = conn.DefaultPort(driver)
	if port := os.Getenv(prefix + "_PORT"); port != "" {
		var err error
		cfg.Port, err = strconv.Atoi(port)
		if err != nil || cfg.Port <= 0 {
			b.Fatalf("invalid port %q for %s", port, driver)
		}
	}
	var query string
	switch driver {
	case conn.DriverPostgres:
		cfg.User = os.Getenv(prefix + "_USER")
		cfg.Password = os.Getenv(prefix + "_PASSWORD")
		query = "SELECT repeat('x', 1048576)"
	case conn.DriverMySQL, conn.DriverMariaDB:
		cfg.User = os.Getenv(prefix + "_USER")
		cfg.Password = os.Getenv(prefix + "_PASSWORD")
		query = "SELECT REPEAT('x', 1048576)"
	case conn.DriverMSSQL:
		cfg.User = os.Getenv(prefix + "_USER")
		cfg.Password = os.Getenv(prefix + "_PASSWORD")
		query = "SELECT REPLICATE(CAST('x' AS varchar(max)), 1048576)"
	case conn.DriverMongo:
		measurementID := strconv.FormatInt(time.Now().UnixNano(), 10)
		collection := "picklock_e6_measure_" + measurementID
		uri := "mongodb://" + net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)) + "/" + cfg.Database
		client, err := mongodriver.Connect(context.Background(), mongooptions.Client().ApplyURI(uri))
		if err != nil {
			b.Fatal(err)
		}
		coll := client.Database(cfg.Database).Collection(collection)
		if _, err := coll.InsertOne(context.Background(), bson.M{"_id": "e6", "payload": payload}); err != nil {
			_ = client.Disconnect(context.Background())
			b.Fatal(err)
		}
		cleanup := func() {
			_ = coll.Drop(context.Background())
			_ = client.Disconnect(context.Background())
		}
		return cfg, "db." + collection + ".find({})", cleanup, true
	case conn.DriverRedis:
		cfg.Database = "0"
		key := "picklock:e6:measurement:" + strconv.FormatInt(time.Now().UnixNano(), 10)
		client := goredis.NewClient(&goredis.Options{Addr: net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))})
		if err := client.Set(context.Background(), key, payload, 0).Err(); err != nil {
			_ = client.Close()
			b.Fatal(err)
		}
		return cfg, "GET " + key, func() {
			_ = client.Del(context.Background(), key).Err()
			_ = client.Close()
		}, true
	case conn.DriverCassandra:
		keyspace := "picklock_e6_measure_" + strconv.FormatInt(time.Now().UnixNano(), 10)
		cfg.Database = keyspace
		cluster := gocql.NewCluster(cfg.Host)
		cluster.Port = cfg.Port
		session, err := cluster.CreateSession()
		if err != nil {
			b.Fatal(err)
		}
		if err := session.Query("CREATE KEYSPACE " + keyspace + " WITH replication = {'class':'SimpleStrategy','replication_factor':1}").Exec(); err != nil {
			session.Close()
			b.Fatal(err)
		}
		session.Close()
		cluster.Keyspace = keyspace
		session, err = cluster.CreateSession()
		if err != nil {
			b.Fatal(err)
		}
		if err := session.Query("CREATE TABLE payloads (id int PRIMARY KEY, body text)").Exec(); err != nil {
			session.Close()
			b.Fatal(err)
		}
		if err := session.Query("INSERT INTO payloads (id, body) VALUES (?, ?)", 1, payload).Exec(); err != nil {
			session.Close()
			b.Fatal(err)
		}
		session.Close()
		cleanup := func() {
			cleanupCluster := *cluster
			cleanupCluster.Keyspace = ""
			if session, err := cleanupCluster.CreateSession(); err == nil {
				_ = session.Query("DROP KEYSPACE IF EXISTS " + keyspace).Exec()
				session.Close()
			}
		}
		return cfg, "SELECT body FROM payloads WHERE id = 1", cleanup, true
	case conn.DriverNeo4j:
		cfg.User = os.Getenv(prefix + "_USER")
		cfg.Password = os.Getenv(prefix + "_PASSWORD")
		query = "RETURN '" + payload + "' AS payload"
	default:
		b.Fatalf("unsupported measurement driver %q", driver)
	}
	return cfg, query, nil, true
}
func e6ResultStatus(data store.DataView) (store.ResultStatus, bool) {
	switch result := data.(type) {
	case *store.TabularData:
		return result.Status, result.Truncated || result.Status.Reason != ""
	case *store.DocumentData:
		return result.Status, result.Status.Reason != ""
	case *store.KeyValueData:
		return result.Status, result.Status.Reason != ""
	case *store.GraphData:
		return result.Status, result.Status.Reason != ""
	case *store.RawTextData:
		return result.Status, result.Status.Reason != ""
	default:
		return store.ResultStatus{}, false
	}
}
