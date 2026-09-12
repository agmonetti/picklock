package mongo

import (
	"context"
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/agmonetti/picklock/internal/conn"
	"github.com/agmonetti/picklock/internal/store"
)

func TestMongo_Registered(t *testing.T) {
	// Check that DriverMongo is registered in the factory registry
	// by attempting to create an invalid host connection (should fail on ping, not 'unknown driver')
	cfg := conn.New(conn.DriverMongo)
	cfg.Host = "127.0.0.1"
	cfg.Port = 65534 // unopened port

	_, err := store.New(cfg)
	if err == nil {
		t.Fatal("expected connection error on unopened port")
	}
	if err.Error() == "unsupported driver: mongo" {
		t.Fatal("DriverMongo not registered in store registry")
	}
}

func TestMongo_ExtractDocID(t *testing.T) {
	oid := primitive.NewObjectID()
	doc := bson.M{"_id": oid, "name": "Alice"}
	if id := extractDocID(doc); id != oid.Hex() {
		t.Errorf("extractDocID = %q, want %q", id, oid.Hex())
	}

	doc2 := bson.M{"_id": "custom-string-id", "num": 42}
	if id := extractDocID(doc2); id != "custom-string-id" {
		t.Errorf("extractDocID = %q, want custom-string-id", id)
	}

	doc3 := bson.M{"no_id": true}
	if id := extractDocID(doc3); id != "unknown" {
		t.Errorf("extractDocID = %q, want unknown", id)
	}
}

func TestMongo_BuildDocSummary(t *testing.T) {
	doc := bson.M{
		"_id":   primitive.NewObjectID(),
		"title": "Clean Code",
		"price": 29.99,
	}
	summary := buildDocSummary(doc)
	if summary == "" || summary == "{}" {
		t.Errorf("summary was empty for doc: %s", summary)
	}
}

func TestMongo_InferBSONType(t *testing.T) {
	tests := []struct {
		val  any
		want string
	}{
		{primitive.NewObjectID(), "objectId"},
		{"hello", "string"},
		{int32(10), "int"},
		{int64(100), "int"},
		{12.34, "double"},
		{true, "bool"},
		{bson.A{1, 2, 3}, "array"},
		{bson.M{"a": 1}, "object"},
		{nil, "null"},
	}

	for _, tt := range tests {
		got := inferBSONType(tt.val)
		if got != tt.want {
			t.Errorf("inferBSONType(%v) = %q, want %q", tt.val, got, tt.want)
		}
	}
}

func TestMongo_MQLPattern(t *testing.T) {
	matches := mqlCallPattern.FindStringSubmatch("db.users.find({ age: { $gt: 18 } })")
	if len(matches) != 4 {
		t.Fatalf("expected 4 match elements, got %d", len(matches))
	}
	if matches[1] != "users" {
		t.Errorf("collection = %q, want users", matches[1])
	}
	if matches[2] != "find" {
		t.Errorf("method = %q, want find", matches[2])
	}

	// Semicolon stripping
	matches2 := mqlCallPattern.FindStringSubmatch("db.orders.countDocuments();")
	if len(matches2) != 4 || matches2[1] != "orders" || matches2[2] != "countDocuments" {
		t.Fatalf("match with semicolon failed: %v", matches2)
	}
}

func TestMongo_ExecutorLanguageAndPrompt(t *testing.T) {
	exec := &MongoExecutor{}
	if exec.Language() != "MQL (BSON / JSON)" {
		t.Errorf("Language = %q", exec.Language())
	}
	if exec.PromptTitle() != "MONGO QUERY" {
		t.Errorf("PromptTitle = %q", exec.PromptTitle())
	}
	if !exec.IsMutation("db.users.insertOne({name: 'Alice'})") {
		t.Error("insertOne must be detected as mutation")
	}
	if !exec.IsMutation("db.users.deleteOne({id: 1})") {
		t.Error("deleteOne must be detected as mutation")
	}
	if !exec.IsMutation("db.users.updateMany({}, {$set: {a: 1}})") {
		t.Error("updateMany must be detected as mutation")
	}
	if exec.IsMutation("db.users.find({})") {
		t.Error("find must not be detected as mutation")
	}
	// words that merely appear inside a filter must not flag reads as writes
	if exec.IsMutation("db.users.find({status: \"insert\"})") {
		t.Error("find with an 'insert' value must not be a mutation")
	}
	if exec.IsMutation("db.users.countDocuments({created: {$gte: 1}})") {
		t.Error("countDocuments must not be detected as mutation")
	}
	if !exec.IsMutation(`{"insert": "users", "documents": [{a: 1}]}`) {
		t.Error("raw JSON insert command must be detected as mutation")
	}
	if !exec.IsMutation(`{"findAndModify": "users", "update": {"a": 1}}`) {
		t.Error("raw JSON findAndModify command must be detected as mutation")
	}
	if exec.IsMutation(`{"find": "users", "limit": 5}`) {
		t.Error("raw JSON find command must not be detected as mutation")
	}
}

func TestMongo_ReadOnlyBlocksMutations(t *testing.T) {
	source := &MongoSource{readOnly: true}
	exec := &MongoExecutor{source: source}

	_, err := exec.Execute(context.Background(), "db.users.insertOne({name: 'Blocked'})", 0, 10)
	if err == nil {
		t.Fatal("expected mutation to be blocked in read-only mode")
	}
}

func TestMongo_ReadOnlyAllowsReads(t *testing.T) {
	// Words like "insert"/"update" inside a filter must still run in read-only
	// mode; they are guarded after the read-only classification, so here we
	// only assert the classification is not a false positive.
	source := &MongoSource{readOnly: true}
	exec := &MongoExecutor{source: source}
	if exec.IsMutation("db.users.find({tags: \"insert updated\"})") {
		t.Fatal("read query with mutation words in the filter was misclassified")
	}
}

func TestMongo_QuoteMQLKeys(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{`{ age: { $gt: 18 } }`, `{"age": {"$gt": 18} }`},
		{`{ age: 18 }`, `{"age": 18}`},
		{`{}`, `{}`},
		{`{"age": 18}`, `{"age": 18}`}, // strict JSON passes through
		{`{ name: "Charlie", role: "admin" }`, `{"name": "Charlie", "role": "admin"}`},
		{`{ name: 'Charlie' }`, `{"name": "Charlie"}`},
		{`{ $and: [ {age: {$gt: 25}}, {age: {$lt: 45}} ] }`,
			`{"$and": [ {"age": {"$gt": 25}}, {"age": {"$lt": 45}} ] }`},
		{`{ status : { $in : [ "active", "pending" ] } }`,
			`{"status" : { "$in" : [ "active", "pending" ] } }`},
		{`{items: [{sku: "a", qty: 2}, {sku: "b", qty: 1}]}`,
			`{"items": [{"sku": "a", "qty": 2}, {"sku": "b", "qty": 1}]}`},
	}
	for _, tc := range tests {
		got, err := quoteMQLKeys(tc.in)
		if err != nil {
			t.Errorf("quoteMQLKeys(%q) error: %v", tc.in, err)
			continue
		}
		if stripSpace(got) != stripSpace(tc.want) {
			t.Errorf("quoteMQLKeys(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func stripSpace(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func TestMongo_ParseMQLDoc(t *testing.T) {
	doc, err := parseMQLDoc(`{ age: { $gt: 18 } }`)
	if err != nil {
		t.Fatalf("parseMQLDoc: %v", err)
	}
	ageField, ok := doc["age"]
	if !ok {
		t.Fatalf("parsed doc missing age: %v", doc)
	}
	if gt, ok := ageField.(bson.M)["$gt"]; !ok || gt != int32(18) {
		t.Errorf("age.$gt = %v (type %T), want int32 18", ageField.(bson.M)["$gt"], gt)
	}

	empty, err := parseMQLDoc("")
	if err != nil || len(empty) != 0 {
		t.Errorf("parseMQLDoc(\"\") = %v, %v", empty, err)
	}

	if _, err := parseMQLDoc(`{unclosed: [`); err == nil {
		t.Error("malformed input must error")
	}
	if _, err := parseMQLDoc(`{nope: nonsense}`); err == nil {
		t.Error("non-JSON/non-shell input must error")
	}
}
