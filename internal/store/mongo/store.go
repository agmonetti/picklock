package mongo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"

	"github.com/agmonetti/picklock/internal/conn"
	"github.com/agmonetti/picklock/internal/store"
)

func init() {
	store.Register(conn.DriverMongo, New)
}

// MongoSource implements store.DataSource for MongoDB.
type MongoSource struct {
	client   *mongo.Client
	database *mongo.Database
	dbName   string
	readOnly bool
	uri      string
}

// New creates and connects a MongoSource.
func New(cfg conn.ConnectionConfig) (store.DataSource, error) {
	uri := cfg.URI
	if uri == "" {
		host := cfg.Host
		if host == "" {
			host = "localhost"
		}
		port := cfg.Port
		if port <= 0 {
			port = 27017
		}
		var userPart string
		if cfg.User != "" {
			if cfg.Password != "" {
				userPart = url.QueryEscape(cfg.User) + ":" + url.QueryEscape(cfg.Password) + "@"
			} else {
				userPart = url.QueryEscape(cfg.User) + "@"
			}
		}
		dbPart := ""
		if cfg.Database != "" {
			dbPart = "/" + cfg.Database
		}
		uri = fmt.Sprintf("mongodb://%s%s:%d%s", userPart, host, port, dbPart)
	}

	clientOpts := options.Client().ApplyURI(uri)
	if cfg.ReadOnly {
		clientOpts.SetReadPreference(readpref.SecondaryPreferred())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, clientOpts)
	if err != nil {
		return nil, fmt.Errorf("mongo connect: %w", err)
	}

	if err := client.Ping(ctx, readpref.PrimaryPreferred()); err != nil {
		_ = client.Disconnect(ctx)
		return nil, fmt.Errorf("mongo ping: %w", err)
	}

	dbName := cfg.Database
	if dbName == "" {
		dbName = "test"
	}

	return &MongoSource{
		client:   client,
		database: client.Database(dbName),
		dbName:   dbName,
		readOnly: cfg.ReadOnly,
		uri:      uri,
	}, nil
}

func (s *MongoSource) Driver() conn.Driver {
	return conn.DriverMongo
}

func (s *MongoSource) Version(ctx context.Context) (string, error) {
	var res bson.M
	err := s.client.Database("admin").RunCommand(ctx, bson.D{primitive.E{Key: "buildInfo", Value: 1}}).Decode(&res)
	if err != nil {
		return "MongoDB", nil
	}
	if v, ok := res["version"].(string); ok {
		return "MongoDB " + v, nil
	}
	return "MongoDB", nil
}

func (s *MongoSource) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.client.Disconnect(ctx)
}

func (s *MongoSource) ReadOnly() bool {
	return s.readOnly
}

func (s *MongoSource) Catalog() store.CatalogDescriptor {
	return store.CatalogDescriptor{
		Title:    "COLLECTIONS",
		ItemNoun: "collection",
		ListObjects: func(ctx context.Context) ([]store.CatalogItem, error) {
			names, err := s.database.ListCollectionNames(ctx, bson.M{})
			if err != nil {
				return nil, fmt.Errorf("list collections: %w", err)
			}
			items := make([]store.CatalogItem, len(names))
			for i, name := range names {
				items[i] = store.CatalogItem{
					Name: name,
				}
			}
			return items, nil
		},
	}
}

func (s *MongoSource) Browse(ctx context.Context, req store.BrowseRequest) (store.BrowseResponse, error) {
	if req.ObjectName == "" {
		return store.BrowseResponse{}, errors.New("no collection specified")
	}

	coll := s.database.Collection(req.ObjectName)
	total, err := coll.EstimatedDocumentCount(ctx)
	if err != nil {
		total, _ = coll.CountDocuments(ctx, bson.M{})
	}

	limit := int64(req.PageSize)
	if limit <= 0 {
		limit = 50
	}
	skip := int64(req.Page) * limit

	findOpts := options.Find().SetLimit(limit).SetSkip(skip)

	cur, err := coll.Find(ctx, bson.M{}, findOpts)
	if err != nil {
		return store.BrowseResponse{}, fmt.Errorf("find: %w", err)
	}
	defer cur.Close(ctx)

	var docs []store.DocumentItem
	for cur.Next(ctx) {
		var doc bson.M
		if err := cur.Decode(&doc); err != nil {
			continue
		}
		idStr := extractDocID(doc)
		jsonBytes, _ := json.MarshalIndent(doc, "", "  ")
		summary := buildDocSummary(doc)

		docs = append(docs, store.DocumentItem{
			ID:      idStr,
			RawJSON: string(jsonBytes),
			Summary: summary,
		})
	}

	hasMore := (skip + int64(len(docs))) < total
	nextCursor := ""
	if hasMore && len(docs) > 0 {
		nextCursor = strconv.FormatInt(int64(req.Page+1), 10)
	}

	docData := &store.DocumentData{
		Documents: docs,
		TotalDocs: total,
	}

	return store.BrowseResponse{
		Data:       docData,
		HasNext:    hasMore,
		TotalCount: total,
		NextCursor: nextCursor,
	}, nil
}

func (s *MongoSource) Inspect(ctx context.Context, name string) (store.InspectionView, error) {
	coll := s.database.Collection(name)

	var stats bson.M
	_ = s.database.RunCommand(ctx, bson.D{primitive.E{Key: "collStats", Value: name}}).Decode(&stats)

	docCount := int64(0)
	totalSize := int64(0)
	avgSize := int64(0)
	indexSize := int64(0)

	if count, ok := stats["count"].(int32); ok {
		docCount = int64(count)
	} else if count, ok := stats["count"].(int64); ok {
		docCount = count
	}
	if size, ok := stats["size"].(int32); ok {
		totalSize = int64(size)
	} else if size, ok := stats["size"].(int64); ok {
		totalSize = size
	}
	if avg, ok := stats["avgObjSize"].(int32); ok {
		avgSize = int64(avg)
	} else if avg, ok := stats["avgObjSize"].(float64); ok {
		avgSize = int64(avg)
	}
	if isize, ok := stats["totalIndexSize"].(int32); ok {
		indexSize = int64(isize)
	} else if isize, ok := stats["totalIndexSize"].(int64); ok {
		indexSize = isize
	}

	// List indexes
	var indexes []store.Index
	cur, err := coll.Indexes().List(ctx)
	if err == nil {
		defer cur.Close(ctx)
		for cur.Next(ctx) {
			var ixDoc bson.M
			if err := cur.Decode(&ixDoc); err == nil {
				ixName, _ := ixDoc["name"].(string)
				uniq, _ := ixDoc["unique"].(bool)
				var cols []string
				if keyMap, ok := ixDoc["key"].(bson.M); ok {
					for k := range keyMap {
						cols = append(cols, k)
					}
				}
				indexes = append(indexes, store.Index{
					Name:    ixName,
					Columns: cols,
					Unique:  uniq,
				})
			}
		}
	}

	// Sample schema fields from first 10 documents
	fieldMap := map[string]string{}
	sampleCur, err := coll.Find(ctx, bson.M{}, options.Find().SetLimit(10))
	if err == nil {
		defer sampleCur.Close(ctx)
		for sampleCur.Next(ctx) {
			var doc bson.M
			if err := sampleCur.Decode(&doc); err == nil {
				for k, v := range doc {
					fieldMap[k] = inferBSONType(v)
				}
			}
		}
	}

	var fields []store.FieldSchema
	for k, t := range fieldMap {
		fields = append(fields, store.FieldSchema{
			Name: k,
			Type: t,
		})
	}

	return &store.DocumentStructure{
		CollectionName: name,
		DocCount:       docCount,
		TotalSize:      totalSize,
		AvgSize:        avgSize,
		IndexSize:      indexSize,
		Indexes:        indexes,
		SampleFields:   fields,
	}, nil
}

func (s *MongoSource) Query() store.QueryExecutor {
	return &MongoExecutor{source: s}
}

// MongoExecutor executes MQL queries and JSON commands.
type MongoExecutor struct {
	source *MongoSource
}

func (e *MongoExecutor) Language() string {
	return "MQL (BSON / JSON)"
}

func (e *MongoExecutor) PromptTitle() string {
	return "MONGO QUERY"
}

func (e *MongoExecutor) Placeholder() string {
	return "db.collection.find({})"
}

// mongoMutationMethods are the collection methods that change data or schema.
var mongoMutationMethods = map[string]bool{
	"insertone": true, "insertmany": true,
	"updateone": true, "updatemany": true, "replaceone": true,
	"deleteone": true, "deletemany": true,
	"findoneanddelete": true, "findoneandreplace": true, "findoneandupdate": true,
	"drop": true, "dropindex": true, "createindex": true,
	"renamecollection": true,
}

// IsMutation reports whether the statement writes data or schema. It inspects
// the called collection method (or the leading key of a raw JSON command), so
// read-only browse filters that merely contain words like "insert" or "update"
// are never flagged as mutations.
func (e *MongoExecutor) IsMutation(stmt string) bool {
	trimmed := strings.TrimSpace(stmt)
	if m := mqlCallPattern.FindStringSubmatch(trimmed); len(m) == 4 {
		return mongoMutationMethods[strings.ToLower(m[2])]
	}
	lower := strings.ToLower(trimmed)
	for _, op := range []string{`"insert"`, `"update"`, `"delete"`, `"findandmodify"`, `"drop"`, `"create"`, `"renamecollection"`, `"dropindexes"`} {
		if strings.HasPrefix(lower, "{"+op) {
			return true
		}
	}
	return false
}

var mqlCallPattern = regexp.MustCompile(`(?i)^db\.([a-zA-Z0-9_\-\.]+)\.([a-zA-Z0-9_]+)\s*\(([\s\S]*)\)\s*;?$`)

// Execute runs a MongoDB MQL statement against the store. The QueryExecutor
// contract passes (buffer, line, maxRows): line is the cursor line (MQL is
// single-statement, so it is ignored) and maxRows caps how many documents a
// find loads into memory.
func (e *MongoExecutor) Execute(ctx context.Context, buffer string, _line int, maxRows int) (store.DataView, error) {
	trimmed := strings.TrimSpace(buffer)
	if trimmed == "" {
		return nil, errors.New("empty query")
	}

	if e.source.readOnly && e.IsMutation(trimmed) {
		return nil, errors.New("mutations are blocked in read-only mode")
	}

	// 1. Check for db.collection.method(...) syntax
	if m := mqlCallPattern.FindStringSubmatch(trimmed); len(m) == 4 {
		collName := m[1]
		method := strings.ToLower(m[2])
		argsRaw := strings.TrimSpace(m[3])
		return e.executeMQLCall(ctx, collName, method, argsRaw, maxRows)
	}

	// 2. Check for raw JSON command: e.g. {"find": "users", "filter": {...}}
	if strings.HasPrefix(trimmed, "{") {
		var doc bson.D
		if err := bson.UnmarshalExtJSON([]byte(trimmed), true, &doc); err != nil {
			return nil, fmt.Errorf("invalid BSON/JSON command: %w", err)
		}
		var res bson.M
		if err := e.source.database.RunCommand(ctx, doc).Decode(&res); err != nil {
			return nil, fmt.Errorf("command execution error: %w", err)
		}
		jsonBytes, _ := json.MarshalIndent(res, "", "  ")
		return &store.DocumentData{
			Documents: []store.DocumentItem{
				{
					ID:      "command_result",
					RawJSON: string(jsonBytes),
					Summary: "Command Result",
				},
			},
			TotalDocs: 1,
		}, nil
	}

	return nil, fmt.Errorf("unsupported MongoDB syntax; use db.<collection>.find({...}) or a JSON command")
}

func (e *MongoExecutor) executeMQLCall(ctx context.Context, collName, method, argsRaw string, maxRows int) (store.DataView, error) {
	coll := e.source.database.Collection(collName)

	switch method {
	case "find":
		filterDoc, err := parseMQLDoc(argsRaw)
		if err != nil {
			return nil, fmt.Errorf("invalid find filter JSON: %w", err)
		}

		findLimit := int64(maxRows)
		if findLimit <= 0 {
			findLimit = 50
		}
		findOpts := options.Find().SetLimit(findLimit).SetSkip(0)

		cur, err := coll.Find(ctx, filterDoc, findOpts)
		if err != nil {
			return nil, fmt.Errorf("find: %w", err)
		}
		defer cur.Close(ctx)

		var docs []store.DocumentItem
		for cur.Next(ctx) {
			var doc bson.M
			if err := cur.Decode(&doc); err != nil {
				continue
			}
			idStr := extractDocID(doc)
			jsonBytes, _ := json.MarshalIndent(doc, "", "  ")
			summary := buildDocSummary(doc)
			docs = append(docs, store.DocumentItem{
				ID:      idStr,
				RawJSON: string(jsonBytes),
				Summary: summary,
			})
		}
		return &store.DocumentData{
			Documents: docs,
			TotalDocs: int64(len(docs)),
		}, nil

	case "countdocuments", "count":
		filterDoc, err := parseMQLDoc(argsRaw)
		if err != nil {
			return nil, fmt.Errorf("invalid count filter JSON: %w", err)
		}
		count, err := coll.CountDocuments(ctx, filterDoc)
		if err != nil {
			return nil, fmt.Errorf("count: %w", err)
		}
		return &store.TabularData{
			Columns:   []string{"count"},
			Rows:      [][]string{{strconv.FormatInt(count, 10)}},
			Affected:  -1, // read query
			TotalRows: -1,
		}, nil

	case "insertone":
		if e.source.readOnly {
			return nil, errors.New("insertOne is blocked in read-only mode")
		}
		doc, err := parseMQLDoc(argsRaw)
		if err != nil {
			return nil, fmt.Errorf("invalid insert JSON: %w", err)
		}
		res, err := coll.InsertOne(ctx, doc)
		if err != nil {
			return nil, fmt.Errorf("insert: %w", err)
		}
		return &store.TabularData{
			Columns:  []string{"inserted_id"},
			Rows:     [][]string{{fmt.Sprint(res.InsertedID)}},
			Affected: 1,
		}, nil

	case "deleteone", "deletemany":
		if e.source.readOnly {
			return nil, errors.New("delete is blocked in read-only mode")
		}
		filter, err := parseMQLDoc(argsRaw)
		if err != nil {
			return nil, fmt.Errorf("invalid delete filter: %w", err)
		}
		var count int64
		if method == "deleteone" {
			res, err := coll.DeleteOne(ctx, filter)
			if err != nil {
				return nil, err
			}
			count = res.DeletedCount
		} else {
			res, err := coll.DeleteMany(ctx, filter)
			if err != nil {
				return nil, err
			}
			count = res.DeletedCount
		}
		return &store.TabularData{
			Affected: count,
		}, nil

	case "drop":
		if e.source.readOnly {
			return nil, errors.New("drop is blocked in read-only mode")
		}
		err := coll.Drop(ctx)
		if err != nil {
			return nil, fmt.Errorf("drop: %w", err)
		}
		return &store.TabularData{
			Affected: 0,
		}, nil

	default:
		return nil, fmt.Errorf("unsupported collection method %q", method)
	}
}

func parseMQLDoc(raw string) (bson.M, error) {
	if raw == "" {
		return bson.M{}, nil
	}
	var doc bson.M
	if err := bson.UnmarshalExtJSON([]byte(raw), true, &doc); err == nil {
		return doc, nil
	}
	if err := json.Unmarshal([]byte(raw), &doc); err == nil {
		return doc, nil
	}
	quoted, err := quoteMQLKeys(raw)
	if err != nil {
		return nil, err
	}
	if err := bson.UnmarshalExtJSON([]byte(quoted), true, &doc); err != nil {
		return nil, fmt.Errorf("invalid BSON/JSON: %w", err)
	}
	return doc, nil
}

// quoteMQLKeys rewrites Mongo-shell style argument text into strict JSON by
// double-quoting unquoted object keys ("age:", "$gt:") and normalizing
// single-quoted string values to double-quoted ones. Already-valid JSON passes
// through unchanged. Values (numbers, booleans, null, nested objects/arrays)
// are left as-is.
func quoteMQLKeys(s string) (string, error) {
	type frame struct {
		obj       bool // top-level container is an object (keys allowed) or array
		expectKey bool
	}
	var out strings.Builder
	stack := []frame{{obj: true, expectKey: false}}
	i, n := 0, len(s)
	for i < n {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			out.WriteByte(c)
			i++
		case c == '{':
			stack = append(stack, frame{obj: true, expectKey: true})
			out.WriteByte(c)
			i++
		case c == '[':
			stack = append(stack, frame{obj: false, expectKey: false})
			out.WriteByte(c)
			i++
		case c == '}' || c == ']':
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
			out.WriteByte(c)
			i++
		case c == '"':
			start := i
			i++
			for i < n {
				if s[i] == '\\' && i+1 < n {
					i += 2
					continue
				}
				if s[i] == '"' {
					i++
					break
				}
				i++
			}
			if i > n {
				return "", fmt.Errorf("unterminated string at byte %d", start)
			}
			out.WriteString(s[start:i])
			stack[len(stack)-1].expectKey = false
		case c == '\'':
			start := i
			i++ // opening quote
			var val strings.Builder
			for i < n && s[i] != '\'' {
				if s[i] == '\\' && i+1 < n {
					val.WriteByte(s[i])
					val.WriteByte(s[i+1])
					i += 2
					continue
				}
				val.WriteByte(s[i])
				i++
			}
			if i >= n {
				return "", fmt.Errorf("unterminated single-quoted string at byte %d", start)
			}
			i++ // closing quote
			enc, err := json.Marshal(val.String())
			if err != nil {
				return "", err
			}
			out.Write(enc)
			stack[len(stack)-1].expectKey = false
		case c == ',':
			out.WriteByte(c)
			i++
			stack[len(stack)-1].expectKey = stack[len(stack)-1].obj
		case c == ':':
			out.WriteByte(c)
			i++
			stack[len(stack)-1].expectKey = false
		case stack[len(stack)-1].expectKey:
			start := i
			for i < n && s[i] != ':' {
				i++
			}
			if i >= n {
				return "", fmt.Errorf("unterminated key at byte %d", start)
			}
			key := strings.TrimSpace(s[start:i])
			if key == "" {
				return "", fmt.Errorf("empty key at byte %d", start)
			}
			if key[0] == '"' || key[0] == '\'' {
				key = strings.Trim(key, `"'`)
			}
			kenc, err := json.Marshal(key)
			if err != nil {
				return "", err
			}
			out.Write(kenc)
			out.WriteByte(':')
			i++ // skip ':'
			stack[len(stack)-1].expectKey = false
		default:
			// primitive value: copy through (numbers, booleans, null, ...)
			out.WriteByte(c)
			i++
		}
	}
	return out.String(), nil
}

func extractDocID(doc bson.M) string {
	if id, ok := doc["_id"]; ok {
		switch v := id.(type) {
		case primitive.ObjectID:
			return v.Hex()
		case string:
			return v
		default:
			return fmt.Sprint(v)
		}
	}
	return "unknown"
}

func buildDocSummary(doc bson.M) string {
	var parts []string
	for k, v := range doc {
		if k == "_id" {
			continue
		}
		valStr := fmt.Sprint(v)
		if len(valStr) > 25 {
			valStr = valStr[:22] + "…"
		}
		parts = append(parts, fmt.Sprintf("%s: %s", k, valStr))
		if len(parts) >= 4 {
			break
		}
	}
	if len(parts) == 0 {
		return "{}"
	}
	return "{ " + strings.Join(parts, ", ") + " }"
}

func inferBSONType(v any) string {
	if v == nil {
		return "null"
	}
	switch v.(type) {
	case primitive.ObjectID:
		return "objectId"
	case string:
		return "string"
	case int32, int64, int:
		return "int"
	case float64, float32:
		return "double"
	case bool:
		return "bool"
	case primitive.DateTime, time.Time:
		return "date"
	case bson.A, []any:
		return "array"
	case bson.M, map[string]any:
		return "object"
	case primitive.Binary:
		return "binData"
	default:
		return fmt.Sprintf("%T", v)
	}
}
