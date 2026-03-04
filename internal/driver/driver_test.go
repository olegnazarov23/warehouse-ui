package driver

import (
	"testing"
	"time"
)

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		input    int64
		expected string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.00 KB"},
		{1048576, "1.00 MB"},
		{1073741824, "1.00 GB"},
		{5368709120, "5.00 GB"},
	}
	for _, tt := range tests {
		got := FormatBytes(tt.input)
		if got != tt.expected {
			t.Errorf("FormatBytes(%d) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		input    time.Duration
		expected string
	}{
		{100 * time.Millisecond, "100ms"},
		{999 * time.Millisecond, "999ms"},
		{1500 * time.Millisecond, "1.5s"},
		{30 * time.Second, "30.0s"},
	}
	for _, tt := range tests {
		got := FormatDuration(tt.input)
		if got != tt.expected {
			t.Errorf("FormatDuration(%v) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestEstimateBQCost(t *testing.T) {
	// 1 TB = 1099511627776 bytes → $5
	cost := EstimateBQCost(1099511627776)
	if cost < 4.99 || cost > 5.01 {
		t.Errorf("expected ~$5 for 1TB, got $%f", cost)
	}

	// 0 bytes → $0
	if EstimateBQCost(0) != 0 {
		t.Error("expected $0 for 0 bytes")
	}
}

func TestAvailableDrivers(t *testing.T) {
	drivers := AvailableDrivers()
	if len(drivers) == 0 {
		t.Error("expected at least one registered driver")
	}
}

func TestNewDriver_Unknown(t *testing.T) {
	_, err := New("nonexistent")
	if err == nil {
		t.Error("expected error for unknown driver type")
	}
}

func TestDriverTypes(t *testing.T) {
	if BigQuery != "bigquery" {
		t.Error("wrong BigQuery const")
	}
	if Postgres != "postgres" {
		t.Error("wrong Postgres const")
	}
	if MySQL != "mysql" {
		t.Error("wrong MySQL const")
	}
	if SQLiteType != "sqlite" {
		t.Error("wrong SQLite const")
	}
}

func TestExplainNodeStructure(t *testing.T) {
	node := ExplainNode{
		Operation:     "Hash Join",
		Table:         "users",
		EstimatedRows: 100,
		Cost:          45.5,
		Details:       "hash cond",
		Children: []ExplainNode{
			{Operation: "Seq Scan", Table: "orders"},
		},
	}

	if node.Operation != "Hash Join" {
		t.Error("wrong operation")
	}
	if len(node.Children) != 1 {
		t.Error("wrong children count")
	}
	if node.Children[0].Table != "orders" {
		t.Error("wrong child table")
	}
}

func TestExplainResultStructure(t *testing.T) {
	result := ExplainResult{
		Plan:    ExplainNode{Operation: "Seq Scan"},
		RawText: "some raw explain output",
	}

	if result.Plan.Operation != "Seq Scan" {
		t.Error("wrong plan operation")
	}
	if result.RawText != "some raw explain output" {
		t.Error("wrong raw text")
	}
}
