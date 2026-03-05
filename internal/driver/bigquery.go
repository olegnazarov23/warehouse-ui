package driver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"cloud.google.com/go/bigquery"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
)

func init() {
	Register(BigQuery, func() Driver { return &BigQueryDriver{} })
}

// BigQueryDriver implements Driver for Google BigQuery.
type BigQueryDriver struct {
	client    *bigquery.Client
	projectID string
	cancel    context.CancelFunc
}

func (d *BigQueryDriver) Type() DriverType         { return BigQuery }
func (d *BigQueryDriver) SupportsCostEstimate() bool { return true }
func (d *BigQueryDriver) QueryLanguage() string     { return "sql" }

func (d *BigQueryDriver) Connect(ctx context.Context, cfg ConnectionConfig) error {
	projectID := cfg.Database
	if p, ok := cfg.Options["project_id"]; ok && p != "" {
		projectID = p
	}

	var opts []option.ClientOption

	// Credentials: file path or inline JSON
	if credPath, ok := cfg.Options["credentials_path"]; ok && credPath != "" {
		opts = append(opts, option.WithCredentialsFile(credPath))
		// Extract project_id from SA JSON if not provided
		if projectID == "" {
			projectID = extractProjectID(credPath)
		}
	} else if credJSON, ok := cfg.Options["credentials_json"]; ok && credJSON != "" {
		opts = append(opts, option.WithCredentialsJSON([]byte(credJSON)))
		if projectID == "" {
			var sa struct{ ProjectID string `json:"project_id"` }
			if json.Unmarshal([]byte(credJSON), &sa) == nil {
				projectID = sa.ProjectID
			}
		}
	}
	// Fallback: GOOGLE_APPLICATION_CREDENTIALS env var (handled by default client)

	if projectID == "" {
		return fmt.Errorf("bigquery: project_id is required (set in Database field or Options)")
	}

	client, err := bigquery.NewClient(ctx, projectID, opts...)
	if err != nil {
		return fmt.Errorf("bigquery connect: %w", err)
	}

	d.client = client
	d.projectID = projectID
	return nil
}

func (d *BigQueryDriver) Disconnect() error {
	if d.client != nil {
		ch := make(chan error, 1)
		go func() { ch <- d.client.Close() }()
		select {
		case err := <-ch:
			return err
		case <-time.After(5 * time.Second):
			// Force nil the client even if Close hangs
			d.client = nil
			return nil
		}
	}
	return nil
}

func (d *BigQueryDriver) Ping(ctx context.Context) error {
	if d.client == nil {
		return fmt.Errorf("not connected")
	}
	// Use a simple dry-run query to verify credentials and access
	type pingResult struct{ err error }
	ch := make(chan pingResult, 1)
	go func() {
		q := d.client.Query("SELECT 1")
		q.DryRun = true
		_, err := q.Run(ctx)
		ch <- pingResult{err}
	}()
	select {
	case r := <-ch:
		return r.err
	case <-time.After(10 * time.Second):
		return fmt.Errorf("BigQuery ping timed out — check service account permissions")
	}
}

func (d *BigQueryDriver) ListDatabases(ctx context.Context) ([]string, error) {
	if d.client == nil {
		return nil, fmt.Errorf("not connected")
	}
	type listResult struct {
		datasets []string
		err      error
	}
	ch := make(chan listResult, 1)
	go func() {
		it := d.client.Datasets(ctx)
		var datasets []string
		for {
			ds, err := it.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				ch <- listResult{nil, fmt.Errorf("list datasets: %w", err)}
				return
			}
			datasets = append(datasets, ds.DatasetID)
		}
		ch <- listResult{datasets, nil}
	}()
	select {
	case r := <-ch:
		return r.datasets, r.err
	case <-time.After(30 * time.Second):
		return nil, fmt.Errorf("listing datasets timed out — check service account permissions")
	}
}

func (d *BigQueryDriver) ListTables(ctx context.Context, dataset string) ([]TableInfo, error) {
	if d.client == nil {
		return nil, fmt.Errorf("not connected")
	}
	listCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	ds := d.client.Dataset(dataset)
	it := ds.Tables(listCtx)

	var tables []TableInfo
	for {
		t, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("list tables: %w", err)
		}
		// Get full metadata for row count and size
		meta, err := t.Metadata(listCtx)
		info := TableInfo{Name: t.TableID}
		if err == nil {
			info.RowCount = int64(meta.NumRows)
			info.SizeBytes = int64(meta.NumBytes)
			switch meta.Type {
			case bigquery.RegularTable:
				info.Type = "table"
			case bigquery.ViewTable:
				info.Type = "view"
			case bigquery.MaterializedView:
				info.Type = "materialized_view"
			case bigquery.ExternalTable:
				info.Type = "external"
			default:
				info.Type = "table"
			}
		} else {
			info.Type = "table"
		}
		tables = append(tables, info)
	}
	return tables, nil
}

func (d *BigQueryDriver) GetTableSchema(ctx context.Context, dataset, table string) (*TableInfo, error) {
	if d.client == nil {
		return nil, fmt.Errorf("not connected")
	}
	schemaCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	ref := d.client.Dataset(dataset).Table(table)
	meta, err := ref.Metadata(schemaCtx)
	if err != nil {
		return nil, fmt.Errorf("get table schema: %w", err)
	}

	info := &TableInfo{
		Name:      table,
		RowCount:  int64(meta.NumRows),
		SizeBytes: int64(meta.NumBytes),
	}
	switch meta.Type {
	case bigquery.ViewTable:
		info.Type = "view"
	case bigquery.MaterializedView:
		info.Type = "materialized_view"
	default:
		info.Type = "table"
	}

	for _, f := range meta.Schema {
		info.Columns = append(info.Columns, Column{
			Name:        f.Name,
			Type:        string(f.Type),
			Nullable:    !f.Required,
			Description: f.Description,
		})
	}
	return info, nil
}

func (d *BigQueryDriver) PreviewTable(ctx context.Context, dataset, table string, limit int) (*QueryResult, error) {
	if limit <= 0 {
		limit = 100
	}
	q := fmt.Sprintf("SELECT * FROM `%s.%s.%s` LIMIT %d",
		d.projectID, dataset, table, limit)
	return d.Execute(ctx, q, limit)
}

func (d *BigQueryDriver) Execute(ctx context.Context, query string, limit int) (*QueryResult, error) {
	if d.client == nil {
		return nil, fmt.Errorf("not connected")
	}

	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	d.cancel = cancel
	defer func() { d.cancel = nil; cancel() }()

	q := d.client.Query(query)
	// Safety cap: 50 GB
	maxBytes := int64(50 * 1024 * 1024 * 1024)
	q.MaxBytesBilled = maxBytes

	start := time.Now()
	job, err := q.Run(queryCtx)
	if err != nil {
		return nil, fmt.Errorf("query run: %w", err)
	}

	status, err := job.Wait(queryCtx)
	if err != nil {
		return nil, fmt.Errorf("query wait: %w", err)
	}
	if status.Err() != nil {
		return nil, fmt.Errorf("query error: %w", status.Err())
	}

	it, err := job.Read(queryCtx)
	if err != nil {
		return nil, fmt.Errorf("query read: %w", err)
	}

	// Build result from iterator
	result := &QueryResult{
		DurationMs: time.Since(start).Milliseconds(),
	}

	// Extract job statistics
	if stats := status.Statistics; stats != nil {
		bp := stats.TotalBytesProcessed
		result.BytesProcessed = &bp
		if details, ok := stats.Details.(*bigquery.QueryStatistics); ok {
			bb := details.TotalBytesBilled
			result.BytesBilled = &bb
			cost := EstimateBQCost(bb)
			result.CostUSD = &cost
			ch := details.CacheHit
			result.CacheHit = &ch
		}
		result.TotalRows = int64(it.TotalRows)
	}

	// Read rows
	count := 0
	for {
		if limit > 0 && count >= limit {
			break
		}
		var row []bigquery.Value
		err := it.Next(&row)
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("row read: %w", err)
		}

		// Set columns from schema on first row
		if count == 0 {
			for _, f := range it.Schema {
				result.Columns = append(result.Columns, f.Name)
				result.ColumnTypes = append(result.ColumnTypes, string(f.Type))
			}
		}

		// Convert BigQuery values to interface{}
		converted := make([]interface{}, len(row))
		for i, v := range row {
			converted[i] = bqValueToInterface(v)
		}
		result.Rows = append(result.Rows, converted)
		count++
	}
	result.RowCount = int64(count)

	return result, nil
}

func (d *BigQueryDriver) DryRun(ctx context.Context, query string) (*DryRunResult, error) {
	if d.client == nil {
		return &DryRunResult{Valid: false, Error: "not connected"}, nil
	}

	dryCtx, dryCancel := context.WithTimeout(ctx, 30*time.Second)
	defer dryCancel()

	q := d.client.Query(query)
	q.DryRun = true

	job, err := q.Run(dryCtx)
	if err != nil {
		return &DryRunResult{Valid: false, Error: err.Error()}, nil
	}

	status := job.LastStatus()
	if status == nil || status.Statistics == nil {
		return &DryRunResult{Valid: true}, nil
	}

	bytesEst := status.Statistics.TotalBytesProcessed
	cost := EstimateBQCost(bytesEst)

	result := &DryRunResult{
		Valid:          true,
		EstimatedBytes: bytesEst,
		EstimatedCost:  cost,
	}

	// Extract statement type and referenced tables from query statistics
	if details, ok := status.Statistics.Details.(*bigquery.QueryStatistics); ok {
		result.StatementType = details.StatementType

		for _, t := range details.ReferencedTables {
			result.ReferencedTables = append(result.ReferencedTables,
				fmt.Sprintf("%s.%s.%s", t.ProjectID, t.DatasetID, t.TableID))
		}
	}

	// Detect statement type from SQL if BQ didn't provide it
	if result.StatementType == "" {
		trimmed := strings.TrimSpace(strings.ToUpper(query))
		for _, prefix := range []string{"SELECT", "INSERT", "UPDATE", "DELETE", "CREATE", "DROP", "ALTER", "WITH"} {
			if strings.HasPrefix(trimmed, prefix) {
				result.StatementType = prefix
				break
			}
		}
	}

	// Cost warnings
	if cost > 50.0 {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("This query will scan %s and cost ~$%.2f. Consider adding filters.",
				FormatBytes(bytesEst), cost))
	} else if cost > 1.0 {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("This query will scan %s (~$%.4f).",
				FormatBytes(bytesEst), cost))
	}

	return result, nil
}

func (d *BigQueryDriver) Cancel() error {
	if d.cancel != nil {
		d.cancel()
	}
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func extractProjectID(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var sa struct{ ProjectID string `json:"project_id"` }
	if json.Unmarshal(data, &sa) == nil {
		return sa.ProjectID
	}
	return ""
}

func bqValueToInterface(v interface{}) interface{} {
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case time.Time:
		return val.Format(time.RFC3339)
	case []bigquery.Value:
		out := make([]interface{}, len(val))
		for i, item := range val {
			out[i] = bqValueToInterface(item)
		}
		return out
	default:
		return val
	}
}
