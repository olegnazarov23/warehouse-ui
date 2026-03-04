package driver

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

func init() {
	Register(SQLiteType, func() Driver { return &SQLiteDriver{} })
}

// SQLiteDriver implements Driver for SQLite databases (pure Go, no CGO).
type SQLiteDriver struct {
	db     *sql.DB
	path   string
	cancel context.CancelFunc
}

func (d *SQLiteDriver) Type() DriverType         { return SQLiteType }
func (d *SQLiteDriver) SupportsCostEstimate() bool { return false }
func (d *SQLiteDriver) QueryLanguage() string     { return "sql" }

func (d *SQLiteDriver) Connect(ctx context.Context, cfg ConnectionConfig) error {
	path := cfg.Database
	if path == "" {
		path = cfg.Options["path"]
	}
	if path == "" {
		return fmt.Errorf("sqlite: database path is required")
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return fmt.Errorf("sqlite connect: %w", err)
	}
	// Enable WAL mode for better concurrent read performance
	if _, err := db.ExecContext(ctx, "PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return fmt.Errorf("sqlite WAL: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return fmt.Errorf("sqlite ping: %w", err)
	}
	d.db = db
	d.path = path
	return nil
}

func (d *SQLiteDriver) Disconnect() error {
	if d.db != nil {
		return d.db.Close()
	}
	return nil
}

func (d *SQLiteDriver) Ping(ctx context.Context) error {
	if d.db == nil {
		return fmt.Errorf("not connected")
	}
	return d.db.PingContext(ctx)
}

func (d *SQLiteDriver) ListDatabases(_ context.Context) ([]string, error) {
	// SQLite has one database per file — return the filename as the "database"
	return []string{"main"}, nil
}

func (d *SQLiteDriver) ListTables(ctx context.Context, _ string) ([]TableInfo, error) {
	rows, err := d.db.QueryContext(ctx,
		"SELECT name, type FROM sqlite_master WHERE type IN ('table', 'view') AND name NOT LIKE 'sqlite_%' ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tables []TableInfo
	for rows.Next() {
		var t TableInfo
		if err := rows.Scan(&t.Name, &t.Type); err != nil {
			return nil, err
		}
		// Get row count for tables (skip views — expensive)
		if t.Type == "table" {
			var count int64
			if err := d.db.QueryRowContext(ctx,
				fmt.Sprintf("SELECT COUNT(*) FROM \"%s\"", t.Name)).Scan(&count); err == nil {
				t.RowCount = count
			}
		}
		tables = append(tables, t)
	}
	return tables, rows.Err()
}

func (d *SQLiteDriver) GetTableSchema(ctx context.Context, _, table string) (*TableInfo, error) {
	rows, err := d.db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(\"%s\")", table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	info := &TableInfo{Name: table, Type: "table"}
	for rows.Next() {
		var cid int
		var c Column
		var notNull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &c.Name, &c.Type, &notNull, &dflt, &pk); err != nil {
			return nil, err
		}
		c.Nullable = notNull == 0
		c.IsPrimary = pk > 0
		info.Columns = append(info.Columns, c)
	}
	return info, rows.Err()
}

func (d *SQLiteDriver) PreviewTable(ctx context.Context, _, table string, limit int) (*QueryResult, error) {
	if limit <= 0 {
		limit = 100
	}
	return d.Execute(ctx, fmt.Sprintf("SELECT * FROM \"%s\" LIMIT %d", table, limit), limit)
}

func (d *SQLiteDriver) Execute(ctx context.Context, query string, limit int) (*QueryResult, error) {
	if d.db == nil {
		return nil, fmt.Errorf("not connected")
	}

	queryCtx, cancel := context.WithCancel(ctx)
	d.cancel = cancel
	defer func() { d.cancel = nil }()

	start := time.Now()
	rows, err := d.db.QueryContext(queryCtx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	result := &QueryResult{Columns: cols}
	count := 0
	for rows.Next() {
		if limit > 0 && count >= limit {
			break
		}
		values := make([]interface{}, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := make([]interface{}, len(cols))
		for i, v := range values {
			row[i] = formatValue(v)
		}
		result.Rows = append(result.Rows, row)
		count++
	}

	result.RowCount = int64(count)
	result.TotalRows = int64(count)
	result.DurationMs = time.Since(start).Milliseconds()
	return result, rows.Err()
}

func (d *SQLiteDriver) DryRun(ctx context.Context, query string) (*DryRunResult, error) {
	if d.db == nil {
		return &DryRunResult{Valid: false, Error: "not connected"}, nil
	}
	_, err := d.db.QueryContext(ctx, "EXPLAIN "+query)
	if err != nil {
		return &DryRunResult{Valid: false, Error: err.Error()}, nil
	}

	result := &DryRunResult{Valid: true}

	// Detect statement type
	trimmed := strings.TrimSpace(strings.ToUpper(query))
	for _, prefix := range []string{"SELECT", "INSERT", "UPDATE", "DELETE", "CREATE", "DROP", "ALTER", "WITH"} {
		if strings.HasPrefix(trimmed, prefix) {
			result.StatementType = prefix
			break
		}
	}

	return result, nil
}

func (d *SQLiteDriver) Cancel() error {
	if d.cancel != nil {
		d.cancel()
	}
	return nil
}
