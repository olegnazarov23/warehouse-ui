package driver

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func init() {
	Register(Postgres, func() Driver { return &PostgresDriver{} })
}

// PostgresDriver implements Driver for PostgreSQL.
type PostgresDriver struct {
	db     *sql.DB
	cancel context.CancelFunc
}

func (d *PostgresDriver) Type() DriverType         { return Postgres }
func (d *PostgresDriver) SupportsCostEstimate() bool { return false }
func (d *PostgresDriver) QueryLanguage() string     { return "sql" }

func (d *PostgresDriver) Connect(ctx context.Context, cfg ConnectionConfig) error {
	ssl := cfg.SSLMode
	if ssl == "" {
		ssl = "disable"
	}

	host := cfg.Host
	port := ""
	if host == "" {
		host = "127.0.0.1"
	}
	// Split host:port if user entered "hostname:5432"
	if strings.Contains(host, ":") {
		parts := strings.SplitN(host, ":", 2)
		host = parts[0]
		port = parts[1]
	}
	if p, ok := cfg.Options["port"]; ok && p != "" {
		port = p
	}

	dsn := fmt.Sprintf("host=%s dbname=%s user=%s password=%s sslmode=%s",
		host, cfg.Database, cfg.Username, cfg.Password, ssl)
	if port != "" {
		dsn += " port=" + port
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("postgres connect: %w", err)
	}
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return fmt.Errorf("postgres ping: %w", err)
	}
	d.db = db
	return nil
}

func (d *PostgresDriver) Disconnect() error {
	if d.db != nil {
		return d.db.Close()
	}
	return nil
}

func (d *PostgresDriver) Ping(ctx context.Context) error {
	if d.db == nil {
		return fmt.Errorf("not connected")
	}
	return d.db.PingContext(ctx)
}

func (d *PostgresDriver) ListDatabases(ctx context.Context) ([]string, error) {
	// For PostgreSQL, return schemas within the connected database.
	// This maps to the UI concept of "databases" as browsable namespaces,
	// since ListTables expects a schema name.
	rows, err := d.db.QueryContext(ctx,
		`SELECT nspname FROM pg_namespace
		 WHERE nspname NOT LIKE 'pg_%'
		   AND nspname != 'information_schema'
		 ORDER BY CASE WHEN nspname = 'public' THEN 0 ELSE 1 END, nspname`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var schemas []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		schemas = append(schemas, name)
	}
	return schemas, rows.Err()
}

func (d *PostgresDriver) ListTables(ctx context.Context, schema string) ([]TableInfo, error) {
	if schema == "" {
		schema = "public"
	}
	query := `
		SELECT c.relname,
			   CASE c.relkind
				   WHEN 'r' THEN 'table'
				   WHEN 'v' THEN 'view'
				   WHEN 'm' THEN 'materialized_view'
				   ELSE 'other'
			   END AS table_type,
			   COALESCE(c.reltuples::bigint, 0) AS row_count,
			   COALESCE(pg_total_relation_size(c.oid), 0) AS size_bytes
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1
		  AND c.relkind IN ('r', 'v', 'm')
		ORDER BY c.relname`

	rows, err := d.db.QueryContext(ctx, query, schema)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tables []TableInfo
	for rows.Next() {
		var t TableInfo
		if err := rows.Scan(&t.Name, &t.Type, &t.RowCount, &t.SizeBytes); err != nil {
			return nil, err
		}
		tables = append(tables, t)
	}
	return tables, rows.Err()
}

func (d *PostgresDriver) GetTableSchema(ctx context.Context, schema, table string) (*TableInfo, error) {
	if schema == "" {
		schema = "public"
	}
	query := `
		SELECT column_name, data_type, is_nullable,
			   COALESCE(column_default, '') AS col_default,
			   COALESCE(
				   (SELECT 'true' FROM information_schema.key_column_usage kcu
					JOIN information_schema.table_constraints tc
					  ON tc.constraint_name = kcu.constraint_name
					 AND tc.table_schema = kcu.table_schema
					WHERE tc.constraint_type = 'PRIMARY KEY'
					  AND kcu.table_schema = c.table_schema
					  AND kcu.table_name = c.table_name
					  AND kcu.column_name = c.column_name
					LIMIT 1), 'false'
			   ) AS is_primary
		FROM information_schema.columns c
		WHERE table_schema = $1 AND table_name = $2
		ORDER BY ordinal_position`

	rows, err := d.db.QueryContext(ctx, query, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	info := &TableInfo{Name: table, Type: "table"}
	for rows.Next() {
		var c Column
		var nullable, isPK, colDefault string
		if err := rows.Scan(&c.Name, &c.Type, &nullable, &colDefault, &isPK); err != nil {
			return nil, err
		}
		c.Nullable = nullable == "YES"
		c.IsPrimary = isPK == "true"
		info.Columns = append(info.Columns, c)
	}
	return info, rows.Err()
}

func (d *PostgresDriver) PreviewTable(ctx context.Context, schema, table string, limit int) (*QueryResult, error) {
	if limit <= 0 {
		limit = 100
	}
	q := fmt.Sprintf("SELECT * FROM %s.%s LIMIT %d",
		quoteIdentPG(schema), quoteIdentPG(table), limit)
	return d.Execute(ctx, q, limit)
}

func (d *PostgresDriver) Execute(ctx context.Context, query string, limit int) (*QueryResult, error) {
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

	result := &QueryResult{
		Columns: cols,
	}
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
		// Convert to string-safe values
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

func (d *PostgresDriver) DryRun(ctx context.Context, query string) (*DryRunResult, error) {
	if d.db == nil {
		return &DryRunResult{Valid: false, Error: "not connected"}, nil
	}

	rows, err := d.db.QueryContext(ctx, "EXPLAIN "+query)
	if err != nil {
		return &DryRunResult{Valid: false, Error: err.Error()}, nil
	}
	defer rows.Close()

	result := &DryRunResult{Valid: true}

	// Parse EXPLAIN output for estimated rows (top-level node)
	firstLine := true
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			continue
		}
		if firstLine {
			firstLine = false
			// Parse "... rows=NNN ..." from the top-level plan node
			if idx := strings.Index(line, "rows="); idx >= 0 {
				numStr := line[idx+5:]
				if end := strings.IndexFunc(numStr, func(r rune) bool { return r < '0' || r > '9' }); end > 0 {
					numStr = numStr[:end]
				}
				if n, err := strconv.ParseInt(numStr, 10, 64); err == nil {
					result.EstimatedRows = n
				}
			}
		}
	}

	// Detect statement type from the SQL
	trimmed := strings.TrimSpace(strings.ToUpper(query))
	for _, prefix := range []string{"SELECT", "INSERT", "UPDATE", "DELETE", "CREATE", "DROP", "ALTER", "WITH"} {
		if strings.HasPrefix(trimmed, prefix) {
			result.StatementType = prefix
			break
		}
	}

	return result, nil
}

func (d *PostgresDriver) Cancel() error {
	if d.cancel != nil {
		d.cancel()
	}
	return nil
}

func quoteIdentPG(s string) string {
	if s == "" {
		return `"public"`
	}
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// formatValue converts sql driver values to JSON-safe types.
func formatValue(v interface{}) interface{} {
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case []byte:
		return string(val)
	case time.Time:
		return val.Format(time.RFC3339)
	default:
		return val
	}
}
