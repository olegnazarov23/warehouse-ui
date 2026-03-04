package driver

import (
	"context"
	"fmt"
	"time"
)

// DriverType identifies the database backend.
type DriverType string

const (
	BigQuery   DriverType = "bigquery"
	Postgres   DriverType = "postgres"
	MySQL      DriverType = "mysql"
	MongoDB    DriverType = "mongodb"
	SQLiteType DriverType = "sqlite"
	ClickHouse DriverType = "clickhouse"
)

// ConnectionConfig holds the parameters needed to connect to any database.
type ConnectionConfig struct {
	ID       string            `json:"id"`
	Type     DriverType        `json:"type"`
	Name     string            `json:"name"`     // user-friendly label
	Host     string            `json:"host"`     // hostname:port
	Database string            `json:"database"` // db name / dataset / project
	Username string            `json:"username"`
	Password string            `json:"password"`
	SSLMode  string            `json:"ssl_mode"` // disable / require / verify-full
	Options  map[string]string `json:"options"`  // driver-specific (e.g. BQ project_id, SA JSON path)
}

// Column describes a single column / field in a table or collection.
type Column struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Nullable    bool   `json:"nullable"`
	Description string `json:"description,omitempty"`
	IsPrimary   bool   `json:"is_primary,omitempty"`
}

// TableInfo describes a table, view, or collection.
type TableInfo struct {
	Name      string   `json:"name"`
	Type      string   `json:"type"` // table / view / collection / materialized_view
	RowCount  int64    `json:"row_count"`
	SizeBytes int64    `json:"size_bytes"`
	Columns   []Column `json:"columns,omitempty"`
}

// QueryResult holds the result of executing a query.
type QueryResult struct {
	Columns        []string        `json:"columns"`
	ColumnTypes    []string        `json:"column_types,omitempty"`
	Rows           [][]interface{} `json:"rows"`
	RowCount       int64           `json:"row_count"`
	TotalRows      int64           `json:"total_rows"` // may exceed RowCount if limited
	DurationMs     int64           `json:"duration_ms"`
	BytesProcessed *int64          `json:"bytes_processed,omitempty"`
	BytesBilled    *int64          `json:"bytes_billed,omitempty"`
	CostUSD        *float64        `json:"cost_usd,omitempty"`
	CacheHit       *bool           `json:"cache_hit,omitempty"`
}

// DryRunResult holds the result of a dry-run / EXPLAIN.
type DryRunResult struct {
	Valid            bool     `json:"valid"`
	EstimatedBytes   int64    `json:"estimated_bytes"`
	EstimatedCost    float64  `json:"estimated_cost_usd"`
	EstimatedRows    int64    `json:"estimated_rows"`
	StatementType    string   `json:"statement_type,omitempty"`
	Error            string   `json:"error,omitempty"`
	Warnings         []string `json:"warnings,omitempty"`
	ReferencedTables []string `json:"referenced_tables,omitempty"`
}

// ExplainNode represents a node in a query execution plan tree.
type ExplainNode struct {
	Operation     string        `json:"operation"`
	Details       string        `json:"details,omitempty"`
	Table         string        `json:"table,omitempty"`
	EstimatedRows int64         `json:"estimated_rows,omitempty"`
	Cost          float64       `json:"cost,omitempty"`
	Children      []ExplainNode `json:"children,omitempty"`
}

// ExplainResult holds a structured query plan.
type ExplainResult struct {
	Plan    ExplainNode `json:"plan"`
	RawText string      `json:"raw_text"`
}

// Driver is the interface every database driver must implement.
type Driver interface {
	// Connection lifecycle
	Connect(ctx context.Context, cfg ConnectionConfig) error
	Disconnect() error
	Ping(ctx context.Context) error

	// Schema discovery
	ListDatabases(ctx context.Context) ([]string, error)
	ListTables(ctx context.Context, database string) ([]TableInfo, error)
	GetTableSchema(ctx context.Context, database, table string) (*TableInfo, error)
	PreviewTable(ctx context.Context, database, table string, limit int) (*QueryResult, error)

	// Query execution
	Execute(ctx context.Context, query string, limit int) (*QueryResult, error)
	DryRun(ctx context.Context, query string) (*DryRunResult, error)
	Cancel() error

	// Metadata
	Type() DriverType
	SupportsCostEstimate() bool
	QueryLanguage() string // "sql" or "mongodb"
}

// ---------------------------------------------------------------------------
// Registry
// ---------------------------------------------------------------------------

type DriverFactory func() Driver

var registry = map[DriverType]DriverFactory{}

// Register adds a driver factory to the global registry.
func Register(t DriverType, f DriverFactory) {
	registry[t] = f
}

// New creates a new (unconnected) driver instance for the given type.
func New(t DriverType) (Driver, error) {
	f, ok := registry[t]
	if !ok {
		return nil, fmt.Errorf("unknown driver type: %s (available: %v)", t, AvailableDrivers())
	}
	return f(), nil
}

// AvailableDrivers returns all registered driver type names.
func AvailableDrivers() []DriverType {
	out := make([]DriverType, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	return out
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// FormatBytes returns a human-readable byte size string.
func FormatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// FormatDuration returns a human-readable duration.
func FormatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

// EstimateBQCost calculates the on-demand BQ cost at $5/TB.
func EstimateBQCost(bytesProcessed int64) float64 {
	tb := float64(bytesProcessed) / (1024 * 1024 * 1024 * 1024)
	return tb * 5.0
}
