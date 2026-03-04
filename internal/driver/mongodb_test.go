package driver

import (
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestParseMongoQuery_Valid(t *testing.T) {
	tests := []struct {
		input      string
		collection string
		method     string
		args       string
	}{
		{`users.find({})`, "users", "find", "{}"},
		{`orders.find({"status": "active"})`, "orders", "find", `{"status": "active"}`},
		{`products.aggregate([{"$match": {"active": true}}])`, "products", "aggregate", `[{"$match": {"active": true}}]`},
		{`logs.countDocuments({"level": "error"})`, "logs", "countDocuments", `{"level": "error"}`},
		{`users.distinct("email", {})`, "users", "distinct", `"email", {}`},
		{`  users.find({})  `, "users", "find", "{}"},
		{`data.count({})`, "data", "count", "{}"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			coll, method, args, err := ParseMongoQuery(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if coll != tt.collection {
				t.Errorf("collection = %q, want %q", coll, tt.collection)
			}
			if method != tt.method {
				t.Errorf("method = %q, want %q", method, tt.method)
			}
			if args != tt.args {
				t.Errorf("args = %q, want %q", args, tt.args)
			}
		})
	}
}

func TestParseMongoQuery_Invalid(t *testing.T) {
	tests := []string{
		"SELECT * FROM users",
		"users",
		"users.find",
		"users.drop()",
		".find({})",
		"",
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			_, _, _, err := ParseMongoQuery(input)
			if err == nil {
				t.Error("expected error for invalid query")
			}
		})
	}
}

func TestSplitJSONArgs(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{`{"a": 1}`, 1},
		{`{"a": 1}, {"b": 1}`, 2},
		{`[{"$match": {"x": [1,2,3]}}]`, 1},
		{`"field", {"a": 1}`, 2},
		{`{}`, 1},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			parts := splitJSONArgs(tt.input)
			if len(parts) != tt.expected {
				t.Errorf("got %d parts, want %d: %v", len(parts), tt.expected, parts)
			}
		})
	}
}

func TestFlattenDocuments(t *testing.T) {
	docs := []bson.D{
		{
			{Key: "_id", Value: "abc123"},
			{Key: "name", Value: "John"},
			{Key: "age", Value: int32(30)},
		},
		{
			{Key: "_id", Value: "def456"},
			{Key: "name", Value: "Jane"},
			{Key: "age", Value: int32(25)},
			{Key: "email", Value: "jane@test.com"},
		},
	}

	columns, rows := FlattenDocuments(docs)

	if len(columns) != 4 {
		t.Fatalf("expected 4 columns, got %d: %v", len(columns), columns)
	}
	if columns[0] != "_id" {
		t.Errorf("expected _id first, got %s", columns[0])
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	// First row should not have email
	if rows[0][3] != nil {
		t.Errorf("expected nil for missing email, got %v", rows[0][3])
	}
	// Second row should have email
	if rows[1][3] != "jane@test.com" {
		t.Errorf("expected jane@test.com, got %v", rows[1][3])
	}
}

func TestFlattenDocuments_Nested(t *testing.T) {
	docs := []bson.D{
		{
			{Key: "name", Value: "John"},
			{Key: "address", Value: bson.D{
				{Key: "city", Value: "NYC"},
				{Key: "zip", Value: "10001"},
			}},
		},
	}

	columns, rows := FlattenDocuments(docs)

	if len(columns) != 3 { // name, address.city, address.zip
		t.Fatalf("expected 3 columns, got %d: %v", len(columns), columns)
	}
	if rows[0][1] != "NYC" {
		t.Errorf("expected NYC for address.city, got %v", rows[0][1])
	}
}

func TestFlattenDocuments_Empty(t *testing.T) {
	columns, rows := FlattenDocuments(nil)
	if columns != nil || rows != nil {
		t.Error("expected nil for empty docs")
	}
}

func TestBsonTypeName(t *testing.T) {
	tests := []struct {
		input    interface{}
		expected string
	}{
		{"hello", "string"},
		{int32(42), "int"},
		{int64(42), "long"},
		{float64(3.14), "double"},
		{true, "bool"},
		{bson.D{}, "object"},
		{bson.A{}, "array"},
		{nil, "null"},
		{time.Now(), "date"},
	}

	for _, tt := range tests {
		got := bsonTypeName(tt.input)
		if got != tt.expected {
			t.Errorf("bsonTypeName(%T) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}
