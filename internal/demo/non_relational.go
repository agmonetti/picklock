package demo

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/gocql/gocql"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
	goredis "github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

var demoNames = []string{
	"Alice Smith", "Bob Jones", "Carol White", "David Brown", "Eve Davis",
	"Frank Miller", "Grace Wilson", "Heidi Moore", "Ivan Taylor", "Judy Anderson",
}

var demoAdjectives = []string{
	"Pro", "Max", "Ultra", "Elite", "Compact", "Wireless", "Smart", "Eco",
}

var demoCategories = []string{
	"Electronics", "Computers", "Audio", "Wearables", "Accessories",
}

// SeedMongo creates example collections (users, products, orders) with rich nested documents.
func SeedMongo(ctx context.Context, client *mongo.Client, dbName string) error {
	db := client.Database(dbName)
	_ = db.Drop(ctx)

	// 1. Users collection
	usersColl := db.Collection("users")
	var userDocs []any
	roles := []string{"admin", "editor", "viewer", "customer"}
	cities := []string{"New York", "San Francisco", "London", "Tokyo", "Berlin"}

	for i := 1; i <= 50; i++ {
		userDocs = append(userDocs, bson.M{
			"_id":        primitive.NewObjectID(),
			"name":       demoNames[i%len(demoNames)],
			"email":      fmt.Sprintf("user%d@example.com", i),
			"role":       roles[i%len(roles)],
			"age":        20 + (i % 45),
			"active":     i%5 != 0,
			"created_at": time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC).Add(time.Duration(i*12) * time.Hour),
			"address": bson.M{
				"city":    cities[i%len(cities)],
				"street":  fmt.Sprintf("%d Elm Street", 100+i),
				"zipcode": 10000 + i,
			},
			"tags": []string{"premium", "verified", "beta-tester"}[0 : (i%3)+1],
		})
	}
	if _, err := usersColl.InsertMany(ctx, userDocs); err != nil {
		return fmt.Errorf("seed mongo users: %w", err)
	}

	// 2. Products collection
	prodColl := db.Collection("products")
	var prodDocs []any
	for i := 1; i <= 30; i++ {
		prodDocs = append(prodDocs, bson.M{
			"_id":      primitive.NewObjectID(),
			"sku":      fmt.Sprintf("SKU-%04d", i),
			"title":    fmt.Sprintf("Product %s (%d)", demoAdjectives[i%len(demoAdjectives)], i),
			"price":    19.99 + float64(i*5),
			"in_stock": 50 - (i % 40),
			"category": demoCategories[i%len(demoCategories)],
			"specs": bson.M{
				"weight_kg": 0.5 + float64(i)*0.1,
				"color":     []string{"black", "silver", "blue", "red"}[i%4],
			},
		})
	}
	if _, err := prodColl.InsertMany(ctx, prodDocs); err != nil {
		return fmt.Errorf("seed mongo products: %w", err)
	}

	// 3. Orders collection
	ordersColl := db.Collection("orders")
	var orderDocs []any
	for i := 1; i <= 40; i++ {
		orderDocs = append(orderDocs, bson.M{
			"_id":          primitive.NewObjectID(),
			"order_number": fmt.Sprintf("ORD-%05d", 10000+i),
			"user_id":      fmt.Sprintf("USER-%04d", 1+(i%50)),
			"status":       []string{"pending", "processing", "shipped", "delivered"}[i%4],
			"total_amount": 49.50 + float64(i*12),
			"items": []bson.M{
				{"sku": fmt.Sprintf("SKU-%04d", 1+(i%30)), "qty": 1 + (i % 3), "unit_price": 25.0},
				{"sku": fmt.Sprintf("SKU-%04d", 1+((i+1)%30)), "qty": 1, "unit_price": 24.5},
			},
			"created_at": time.Now().Add(-time.Duration(i*3) * time.Hour),
		})
	}
	if _, err := ordersColl.InsertMany(ctx, orderDocs); err != nil {
		return fmt.Errorf("seed mongo orders: %w", err)
	}

	return nil
}

// SeedRedis creates keys covering strings, hashes, lists, sets, and sorted sets.
func SeedRedis(ctx context.Context, client *goredis.Client) error {
	_ = client.FlushDB(ctx).Err()

	// 1. Strings
	_ = client.Set(ctx, "app:config:title", "Picklock Multi-Paradigm Browser", 0).Err()
	_ = client.Set(ctx, "app:config:version", "2.0.0-experimental", 0).Err()
	_ = client.Set(ctx, "session:token:1001", "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...", 3600*time.Second).Err()
	_ = client.Set(ctx, "cache:user:count", "15420", 300*time.Second).Err()

	// 2. Hashes
	for i := 1; i <= 10; i++ {
		key := fmt.Sprintf("user:%d", 1000+i)
		_ = client.HSet(ctx, key, map[string]any{
			"id":         1000 + i,
			"name":       demoNames[i%len(demoNames)],
			"email":      fmt.Sprintf("user%d@example.com", i),
			"role":       "engineer",
			"logins":     i * 7,
			"created_at": time.Now().Add(-time.Duration(i*24) * time.Hour).Format(time.RFC3339),
		}).Err()
	}

	// 3. Lists
	for i := 1; i <= 25; i++ {
		_ = client.RPush(ctx, "queue:tasks", fmt.Sprintf("task-%04d: send notification email", i)).Err()
		_ = client.RPush(ctx, "logs:recent", fmt.Sprintf("[%s] INFO user%d action processed", time.Now().Format("15:04:05"), i)).Err()
	}

	// 4. Sets
	for _, tag := range []string{"golang", "redis", "nosql", "terminal", "tui", "database", "fast", "cli"} {
		_ = client.SAdd(ctx, "tags:popular", tag).Err()
	}
	for i := 1; i <= 15; i++ {
		_ = client.SAdd(ctx, "online:users", fmt.Sprintf("user:%d", 1000+i)).Err()
	}

	// 5. Sorted Sets (ZSets)
	for i := 1; i <= 20; i++ {
		_ = client.ZAdd(ctx, "leaderboard:points", goredis.Z{
			Score:  float64(500 + i*75),
			Member: fmt.Sprintf("player_%s", demoNames[i%len(demoNames)]),
		}).Err()
		_ = client.ZAdd(ctx, "metrics:latency_ms", goredis.Z{
			Score:  float64(12 + i*2),
			Member: fmt.Sprintf("endpoint_%d", i),
		}).Err()
	}

	return nil
}

// SeedCassandra creates keyspace picklock_demo and tables with Partition + Clustering keys.
func SeedCassandra(session *gocql.Session, keyspace string) error {
	stmts := []string{
		fmt.Sprintf("CREATE KEYSPACE IF NOT EXISTS %s WITH replication = {'class': 'SimpleStrategy', 'replication_factor': 1}", keyspace),
		fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s.users_by_country (country text, user_id int, name text, email text, age int, PRIMARY KEY ((country), user_id))", keyspace),
		fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s.sensor_readings (sensor_id text, recorded_at timestamp, temperature double, humidity double, status text, PRIMARY KEY ((sensor_id), recorded_at)) WITH CLUSTERING ORDER BY (recorded_at DESC)", keyspace),
		fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s.store_orders (store_id int, created_at timestamp, order_id text, customer text, amount double, PRIMARY KEY ((store_id), created_at, order_id)) WITH CLUSTERING ORDER BY (created_at DESC, order_id ASC)", keyspace),
	}

	for _, s := range stmts {
		if err := session.Query(s).Exec(); err != nil {
			return fmt.Errorf("cassandra ddl: %w", err)
		}
	}

	// Seed users_by_country
	countries := []string{"US", "GB", "DE", "FR", "JP", "BR"}
	for i := 1; i <= 50; i++ {
		c := countries[i%len(countries)]
		q := fmt.Sprintf("INSERT INTO %s.users_by_country (country, user_id, name, email, age) VALUES (?, ?, ?, ?, ?)", keyspace)
		_ = session.Query(q, c, 1000+i, demoNames[i%len(demoNames)], fmt.Sprintf("user%d@example.com", i), 20+(i%40)).Exec()
	}

	// Seed sensor_readings
	sensors := []string{"SEN-NORTH-01", "SEN-SOUTH-02", "SEN-EAST-03", "SEN-WEST-04"}
	baseTime := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	for i := 1; i <= 60; i++ {
		sID := sensors[i%len(sensors)]
		tStamp := baseTime.Add(time.Duration(i*10) * time.Minute)
		q := fmt.Sprintf("INSERT INTO %s.sensor_readings (sensor_id, recorded_at, temperature, humidity, status) VALUES (?, ?, ?, ?, ?)", keyspace)
		_ = session.Query(q, sID, tStamp, 21.5+float64(i%15)*0.4, 45.0+float64(i%20)*0.5, "OK").Exec()
	}

	return nil
}

// SeedNeo4j creates Person, Movie, and Company nodes with ACTED_IN, DIRECTED, and WORKS_AT relationships.
func SeedNeo4j(ctx context.Context, driver neo4jdriver.DriverWithContext, dbName string) error {
	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{
		DatabaseName: dbName,
		AccessMode:   neo4jdriver.AccessModeWrite,
	})
	defer session.Close(ctx)

	// Clean existing demo graph
	_, _ = session.Run(ctx, "MATCH (n) DETACH DELETE n", nil)

	cypherInit := `
		// Create Movies
		CREATE (m1:Movie {title: 'The Matrix', released: 1999, tagline: 'Welcome to the Real World'})
		CREATE (m2:Movie {title: 'Inception', released: 2010, tagline: 'Your mind is the scene of the crime'})
		CREATE (m3:Movie {title: 'Interstellar', released: 2014, tagline: 'Mankind was born on Earth. It was never meant to die here.'})
		CREATE (m4:Movie {title: 'The Dark Knight', released: 2008, tagline: 'Why so serious?'})

		// Create People
		CREATE (p1:Person {name: 'Keanu Reeves', born: 1964})
		CREATE (p2:Person {name: 'Carrie-Anne Moss', born: 1967})
		CREATE (p3:Person {name: 'Leonardo DiCaprio', born: 1974})
		CREATE (p4:Person {name: 'Christopher Nolan', born: 1970})
		CREATE (p5:Person {name: 'Christian Bale', born: 1974})
		CREATE (p6:Person {name: 'Matthew McConaughey', born: 1969})

		// Create Companies
		CREATE (c1:Company {name: 'Warner Bros.', founded: 1923, hq: 'Burbank, CA'})
		CREATE (c2:Company {name: 'Syncopy Films', founded: 2001, hq: 'London, UK'})

		// Create Relationships
		CREATE (p1)-[:ACTED_IN {roles: ['Neo']}]->(m1)
		CREATE (p2)-[:ACTED_IN {roles: ['Trinity']}]->(m1)
		CREATE (p3)-[:ACTED_IN {roles: ['Cobb']}]->(m2)
		CREATE (p4)-[:DIRECTED]->(m2)
		CREATE (p4)-[:DIRECTED]->(m3)
		CREATE (p4)-[:DIRECTED]->(m4)
		CREATE (p5)-[:ACTED_IN {roles: ['Bruce Wayne', 'Batman']}]->(m4)
		CREATE (p6)-[:ACTED_IN {roles: ['Cooper']}]->(m3)

		CREATE (p4)-[:WORKS_AT {role: 'Co-Founder'}]->(c2)
		CREATE (m1)-[:PRODUCED_BY]->(c1)
		CREATE (m2)-[:PRODUCED_BY]->(c1)
	`
	_, err := session.Run(ctx, cypherInit, nil)
	if err != nil {
		return fmt.Errorf("seed neo4j: %w", err)
	}

	// Add more nodes for pagination
	for i := 1; i <= 20; i++ {
		extraCypher := `
			CREATE (u:Person {name: $name, born: $born})
			CREATE (c:Company {name: $company, founded: $founded, hq: 'Global'})
			CREATE (u)-[:WORKS_AT {role: 'Engineer'}]->(c)
		`
		_, _ = session.Run(ctx, extraCypher, map[string]any{
			"name":    demoNames[i%len(demoNames)] + " " + strconv.Itoa(i),
			"born":    1980 + (i % 20),
			"company": fmt.Sprintf("Tech Corp %d", i),
			"founded": 2000 + (i % 24),
		})
	}

	return nil
}
