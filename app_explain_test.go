package main

import (
	"encoding/json"
	"testing"

	"warehouse-ui/internal/driver"
)

func TestParsePostgresExplainJSON(t *testing.T) {
	raw := `[{
		"Plan": {
			"Node Type": "Hash Join",
			"Join Type": "Inner",
			"Total Cost": 123.45,
			"Plan Rows": 500,
			"Relation Name": "",
			"Hash Cond": "(a.id = b.id)",
			"Plans": [
				{
					"Node Type": "Seq Scan",
					"Relation Name": "users",
					"Total Cost": 50.0,
					"Plan Rows": 1000,
					"Plans": []
				},
				{
					"Node Type": "Hash",
					"Total Cost": 30.0,
					"Plan Rows": 200,
					"Plans": [
						{
							"Node Type": "Seq Scan",
							"Relation Name": "orders",
							"Total Cost": 25.0,
							"Plan Rows": 200
						}
					]
				}
			]
		}
	}]`

	node := parsePostgresExplainJSON(raw)

	if node.Operation != "Hash Join" {
		t.Errorf("expected Hash Join, got %s", node.Operation)
	}
	if node.Cost != 123.45 {
		t.Errorf("expected cost 123.45, got %f", node.Cost)
	}
	if node.EstimatedRows != 500 {
		t.Errorf("expected 500 rows, got %d", node.EstimatedRows)
	}
	if len(node.Children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(node.Children))
	}
	if node.Children[0].Operation != "Seq Scan" {
		t.Errorf("expected Seq Scan child, got %s", node.Children[0].Operation)
	}
	if node.Children[0].Table != "users" {
		t.Errorf("expected users table, got %s", node.Children[0].Table)
	}
	if node.Children[1].Children[0].Table != "orders" {
		t.Errorf("expected orders in nested child, got %s", node.Children[1].Children[0].Table)
	}
}

func TestParsePostgresExplainJSON_Invalid(t *testing.T) {
	node := parsePostgresExplainJSON("not json")
	if node.Operation != "Query Plan" {
		t.Errorf("expected fallback 'Query Plan', got %s", node.Operation)
	}
	if node.Details != "not json" {
		t.Errorf("expected raw text in details, got %s", node.Details)
	}
}

func TestParseMySQLExplainJSON(t *testing.T) {
	raw := `{
		"query_block": {
			"select_id": 1,
			"table": {
				"table_name": "products",
				"access_type": "ref",
				"rows_examined_per_scan": 42,
				"key": "idx_category",
				"attached_condition": "category_id = 5"
			},
			"ordering_operation": {
				"using_filesort": true
			}
		}
	}`

	node := parseMySQLExplainJSON(raw)

	if node.Operation != "Query Block" {
		t.Errorf("expected Query Block, got %s", node.Operation)
	}
	if len(node.Children) != 2 {
		t.Fatalf("expected 2 children (table + filesort), got %d", len(node.Children))
	}
	if node.Children[0].Table != "products" {
		t.Errorf("expected products table, got %s", node.Children[0].Table)
	}
	if node.Children[0].EstimatedRows != 42 {
		t.Errorf("expected 42 rows, got %d", node.Children[0].EstimatedRows)
	}
	if node.Children[1].Operation != "Filesort" {
		t.Errorf("expected Filesort, got %s", node.Children[1].Operation)
	}
}

func TestParseSQLiteExplainPlan(t *testing.T) {
	// Simulating EXPLAIN QUERY PLAN output
	result := &driver.QueryResult{
		Columns: []string{"id", "parent", "notused", "detail"},
		Rows: [][]interface{}{
			{float64(2), float64(0), float64(0), "SCAN users"},
			{float64(3), float64(0), float64(0), "SEARCH orders USING INDEX idx_user_id"},
		},
	}

	node := parseSQLiteExplainPlan(result)

	if node.Operation != "Query Plan" {
		t.Errorf("expected Query Plan root, got %s", node.Operation)
	}
	if len(node.Children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(node.Children))
	}
	if node.Children[0].Operation != "SCAN users" {
		t.Errorf("expected 'SCAN users', got %s", node.Children[0].Operation)
	}
	if node.Children[1].Operation != "SEARCH orders USING INDEX idx_user_id" {
		t.Errorf("expected SEARCH orders, got %s", node.Children[1].Operation)
	}
}

func TestToInt(t *testing.T) {
	tests := []struct {
		input    interface{}
		expected int
		ok       bool
	}{
		{float64(42), 42, true},
		{int64(7), 7, true},
		{int(3), 3, true},
		{"not a number", 0, false},
		{nil, 0, false},
	}
	for _, tt := range tests {
		got, ok := toInt(tt.input)
		if got != tt.expected || ok != tt.ok {
			t.Errorf("toInt(%v) = %d, %v; want %d, %v", tt.input, got, ok, tt.expected, tt.ok)
		}
	}
}

func TestExplainNodeJSON(t *testing.T) {
	node := driver.ExplainNode{
		Operation:     "Seq Scan",
		Table:         "users",
		EstimatedRows: 100,
		Cost:          25.5,
		Children: []driver.ExplainNode{
			{Operation: "Filter", Details: "age > 18"},
		},
	}

	data, err := json.Marshal(node)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded driver.ExplainNode
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if decoded.Operation != "Seq Scan" {
		t.Errorf("expected Seq Scan, got %s", decoded.Operation)
	}
	if decoded.Table != "users" {
		t.Errorf("expected users, got %s", decoded.Table)
	}
	if len(decoded.Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(decoded.Children))
	}
	if decoded.Children[0].Details != "age > 18" {
		t.Errorf("expected 'age > 18', got %s", decoded.Children[0].Details)
	}
}
