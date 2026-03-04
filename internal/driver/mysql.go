package driver

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

func init() {
	Register(MySQL, func() Driver { return &MySQLDriver{} })
}

// MySQLDriver implements Driver for MySQL / MariaDB.
type MySQLDriver struct {
	db     *sql.DB
	cancel context.CancelFunc
}

func (d *MySQLDriver) Type() DriverType         { return MySQL }
func (d *MySQLDriver) SupportsCostEstimate() bool { return false }
func (d *MySQLDriver) QueryLanguage() string     { return "sql" }

func (d *MySQLDriver) Connect(ctx context.Context, cfg ConnectionConfig) error {
	host := cfg.Host
	if host == "" {
		host = "127.0.0.1:3306"
	}
	if !strings.Contains(host, ":") {
		host += ":3306"
	}

	tls := "false"
	if cfg.SSLMode == "require" || cfg.SSLMode == "verify-full" {
		tls = "true"
	}
	dsn := fmt.Sprintf("%s:%s@tcp(%s)/%s?parseTime=true&tls=%s",
		cfg.Username, cfg.Password, host, cfg.Database, tls)

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("mysql connect: %w", err)
	}
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return fmt.Errorf("mysql ping: %w", err)
	}
	d.db = db
	return nil
}

func (d *MySQLDriver) Disconnect() error {
	if d.db != nil {
		return d.db.Close()
	}
	return nil
}

func (d *MySQLDriver) Ping(ctx context.Context) error {
	if d.db == nil {
		return fmt.Errorf("not connected")
	}
	return d.db.PingContext(ctx)
}

func (d *MySQLDriver) ListDatabases(ctx context.Context) ([]string, error) {
	rows, err := d.db.QueryContext(ctx, "SHOW DATABASES")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var dbs []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		// Skip system databases
		if name == "information_schema" || name == "performance_schema" || name == "mysql" || name == "sys" {
			continue
		}
		dbs = append(dbs, name)
	}
	return dbs, rows.Err()
}

func (d *MySQLDriver) ListTables(ctx context.Context, database string) ([]TableInfo, error) {
	query := `
		SELECT TABLE_NAME, TABLE_TYPE,
			   COALESCE(TABLE_ROWS, 0),
			   COALESCE(DATA_LENGTH + INDEX_LENGTH, 0) AS size_bytes
		FROM information_schema.TABLES
		WHERE TABLE_SCHEMA = ?
		ORDER BY TABLE_NAME`

	rows, err := d.db.QueryContext(ctx, query, database)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tables []TableInfo
	for rows.Next() {
		var t TableInfo
		var tableType string
		if err := rows.Scan(&t.Name, &tableType, &t.RowCount, &t.SizeBytes); err != nil {
			return nil, err
		}
		switch tableType {
		case "BASE TABLE":
			t.Type = "table"
		case "VIEW":
			t.Type = "view"
		default:
			t.Type = strings.ToLower(tableType)
		}
		tables = append(tables, t)
	}
	return tables, rows.Err()
}

func (d *MySQLDriver) GetTableSchema(ctx context.Context, database, table string) (*TableInfo, error) {
	query := `
		SELECT COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE, COLUMN_KEY, COLUMN_COMMENT
		FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
		ORDER BY ORDINAL_POSITION`

	rows, err := d.db.QueryContext(ctx, query, database, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	info := &TableInfo{Name: table, Type: "table"}
	for rows.Next() {
		var c Column
		var nullable, key, comment string
		if err := rows.Scan(&c.Name, &c.Type, &nullable, &key, &comment); err != nil {
			return nil, err
		}
		c.Nullable = nullable == "YES"
		c.IsPrimary = key == "PRI"
		c.Description = comment
		info.Columns = append(info.Columns, c)
	}
	return info, rows.Err()
}

func (d *MySQLDriver) PreviewTable(ctx context.Context, database, table string, limit int) (*QueryResult, error) {
	if limit <= 0 {
		limit = 100
	}
	q := fmt.Sprintf("SELECT * FROM `%s`.`%s` LIMIT %d",
		escapeMySQL(database), escapeMySQL(table), limit)
	return d.Execute(ctx, q, limit)
}

func (d *MySQLDriver) Execute(ctx context.Context, query string, limit int) (*QueryResult, error) {
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
	colTypes, _ := rows.ColumnTypes()

	result := &QueryResult{Columns: cols}
	for _, ct := range colTypes {
		result.ColumnTypes = append(result.ColumnTypes, ct.DatabaseTypeName())
	}

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

func (d *MySQLDriver) DryRun(ctx context.Context, query string) (*DryRunResult, error) {
	if d.db == nil {
		return &DryRunResult{Valid: false, Error: "not connected"}, nil
	}
	rows, err := d.db.QueryContext(ctx, "EXPLAIN "+query)
	if err != nil {
		return &DryRunResult{Valid: false, Error: err.Error()}, nil
	}
	defer rows.Close()

	result := &DryRunResult{Valid: true}

	// Parse EXPLAIN output — MySQL returns rows with a "rows" column
	cols, _ := rows.Columns()
	rowsIdx := -1
	for i, c := range cols {
		if strings.EqualFold(c, "rows") {
			rowsIdx = i
			break
		}
	}
	if rowsIdx >= 0 {
		for rows.Next() {
			vals := make([]interface{}, len(cols))
			ptrs := make([]interface{}, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err == nil {
				if v, ok := vals[rowsIdx].(int64); ok && v > result.EstimatedRows {
					result.EstimatedRows = v
				}
			}
		}
	}

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

func (d *MySQLDriver) Cancel() error {
	if d.cancel != nil {
		d.cancel()
	}
	return nil
}

func escapeMySQL(s string) string {
	return strings.ReplaceAll(s, "`", "``")
}
