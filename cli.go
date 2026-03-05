package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"text/tabwriter"

	"warehouse-ui/internal/driver"

	"github.com/google/uuid"
)

// Global output format flag (json or table).
var outputFormat = "json"

// cliRun dispatches CLI subcommands. Returns exit code (0 = success, 1 = error).
func cliRun(cmd string, args []string) int {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	// Extract --format flag before subcommand parsing
	args = extractFormatFlag(args)

	switch cmd {
	case "connect":
		return cmdConnect(ctx, args)
	case "disconnect":
		return cmdDisconnect(ctx, args)
	case "status":
		return cmdStatus(ctx, args)
	case "query":
		return cmdQuery(ctx, args)
	case "schema":
		return cmdSchema(ctx, args)
	case "dry-run":
		return cmdDryRun(ctx, args)
	case "ai":
		return cmdAI(ctx, args)
	case "connections":
		return cmdConnections(ctx, args)
	case "history":
		return cmdHistory(ctx, args)
	case "mcp":
		return cmdMCP(ctx)
	case "version":
		return cmdVersion()
	default:
		cliError("unknown command: %s", cmd)
		return 1
	}
}

// extractFormatFlag pulls --format from args and sets outputFormat global.
func extractFormatFlag(args []string) []string {
	var filtered []string
	for i := 0; i < len(args); i++ {
		if args[i] == "--format" && i+1 < len(args) {
			outputFormat = args[i+1]
			i++
		} else if strings.HasPrefix(args[i], "--format=") {
			outputFormat = strings.TrimPrefix(args[i], "--format=")
		} else {
			filtered = append(filtered, args[i])
		}
	}
	return filtered
}

// --- Helpers ---

func cliJSON(v interface{}) {
	if outputFormat == "table" {
		cliTable(v)
		return
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func cliError(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	_ = json.NewEncoder(os.Stderr).Encode(map[string]string{"error": msg})
}

// cliTable renders data as a human-readable table.
func cliTable(v interface{}) {
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	defer w.Flush()

	switch data := v.(type) {
	case *driver.QueryResult:
		if data == nil || len(data.Columns) == 0 {
			fmt.Fprintln(w, "(no results)")
			return
		}
		// Header
		fmt.Fprintln(w, strings.Join(data.Columns, "\t"))
		// Separator
		seps := make([]string, len(data.Columns))
		for i, col := range data.Columns {
			seps[i] = strings.Repeat("-", max(len(col), 4))
		}
		fmt.Fprintln(w, strings.Join(seps, "\t"))
		// Rows ([][]interface{})
		for _, row := range data.Rows {
			vals := make([]string, len(data.Columns))
			for i := range data.Columns {
				if i < len(row) {
					vals[i] = fmt.Sprintf("%v", row[i])
				}
			}
			fmt.Fprintln(w, strings.Join(vals, "\t"))
		}
		fmt.Fprintf(w, "\n(%d rows, %s)\n", data.RowCount, formatDurationMs(float64(data.DurationMs)))

	case []driver.TableInfo:
		fmt.Fprintln(w, "TABLE\tROWS\tCOLUMNS")
		fmt.Fprintln(w, "-----\t----\t-------")
		for _, t := range data {
			fmt.Fprintf(w, "%s\t%d\t%d\n", t.Name, t.RowCount, len(t.Columns))
		}

	case *driver.TableInfo:
		if data == nil {
			return
		}
		fmt.Fprintf(w, "Table: %s (%d rows)\n\n", data.Name, data.RowCount)
		fmt.Fprintln(w, "COLUMN\tTYPE\tNULLABLE")
		fmt.Fprintln(w, "------\t----\t--------")
		for _, c := range data.Columns {
			fmt.Fprintf(w, "%s\t%s\t%v\n", c.Name, c.Type, c.Nullable)
		}

	case []string:
		for _, s := range data {
			fmt.Fprintln(w, s)
		}

	default:
		// Fallback: marshal to JSON if table format not supported for this type
		b, _ := json.MarshalIndent(v, "", "  ")
		fmt.Fprintln(os.Stdout, string(b))
	}
}

func formatDurationMs(ms float64) string {
	if ms < 1 {
		return "<1ms"
	}
	if ms < 1000 {
		return fmt.Sprintf("%.0fms", ms)
	}
	return fmt.Sprintf("%.2fs", ms/1000)
}

func initHeadlessApp(ctx context.Context) (*App, error) {
	app := NewApp()
	if err := app.StartupHeadless(ctx); err != nil {
		return nil, err
	}
	return app, nil
}

func reconnectActive(app *App) error {
	connID, err := app.store.GetSetting("cli_active_connection")
	if err != nil || connID == "" {
		return fmt.Errorf("not connected — run 'warehouse-ui connect' first")
	}
	return app.ReconnectFromStore(connID)
}

// reconnectActiveOrEnv tries DATABASE_URL env var first, then stored connection.
func reconnectActiveOrEnv(app *App) error {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL != "" {
		cfg, err := app.ParseConnectionString(dbURL, "")
		if err != nil {
			return fmt.Errorf("invalid DATABASE_URL: %v", err)
		}
		cfg.ID = "env-database-url"
		cfg.Name = "DATABASE_URL"
		_, err = app.Connect(*cfg)
		return err
	}
	return reconnectActive(app)
}

func parseOptionFlags(args []string) map[string]string {
	opts := make(map[string]string)
	for i := 0; i < len(args); i++ {
		if args[i] == "--option" && i+1 < len(args) {
			parts := strings.SplitN(args[i+1], "=", 2)
			if len(parts) == 2 {
				opts[parts[0]] = parts[1]
			}
			i++
		}
	}
	return opts
}

// --- Commands ---

func cmdConnect(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("connect", flag.ContinueOnError)
	connType := fs.String("type", "", "Driver type: postgres, mysql, sqlite, bigquery, mongodb, clickhouse")
	host := fs.String("host", "", "Host:port")
	database := fs.String("database", "", "Database name or file path")
	user := fs.String("user", "", "Username")
	pass := fs.String("password", "", "Password")
	sslMode := fs.String("ssl-mode", "", "SSL mode")
	connURL := fs.String("url", "", "Connection URL (alternative to individual flags)")
	name := fs.String("name", "", "Connection label")

	if err := fs.Parse(args); err != nil {
		cliError("invalid flags: %v", err)
		return 1
	}

	app, err := initHeadlessApp(ctx)
	if err != nil {
		cliError("init failed: %v", err)
		return 1
	}
	defer app.ShutdownHeadless()

	var cfg driver.ConnectionConfig

	if *connURL != "" {
		parsed, err := app.ParseConnectionString(*connURL, "")
		if err != nil {
			cliError("invalid URL: %v", err)
			return 1
		}
		cfg = *parsed
	} else {
		if *connType == "" {
			cliError("--type or --url is required")
			return 1
		}
		cfg = driver.ConnectionConfig{
			Type:     driver.DriverType(*connType),
			Host:     *host,
			Database: *database,
			Username: *user,
			Password: *pass,
			SSLMode:  *sslMode,
			Options:  parseOptionFlags(args),
		}
	}

	if *name != "" {
		cfg.Name = *name
	}
	if cfg.Name == "" {
		if cfg.Host != "" {
			cfg.Name = fmt.Sprintf("%s@%s/%s", cfg.Type, cfg.Host, cfg.Database)
		} else {
			cfg.Name = fmt.Sprintf("%s/%s", cfg.Type, cfg.Database)
		}
	}
	cfg.ID = uuid.New().String()

	// Validate by connecting
	status, err := app.Connect(cfg)
	if err != nil {
		cliError("connect failed: %v", err)
		return 1
	}

	// Save connection and mark as active for CLI
	if err := app.SaveConnection(cfg); err != nil {
		cliError("save connection failed: %v", err)
		return 1
	}
	_ = app.store.SetSetting("cli_active_connection", cfg.ID)
	_ = app.Disconnect()

	cliJSON(status)
	return 0
}

func cmdDisconnect(ctx context.Context, args []string) int {
	app, err := initHeadlessApp(ctx)
	if err != nil {
		cliError("init failed: %v", err)
		return 1
	}
	defer app.ShutdownHeadless()

	_ = app.store.SetSetting("cli_active_connection", "")
	cliJSON(map[string]string{"status": "disconnected"})
	return 0
}

func cmdStatus(ctx context.Context, args []string) int {
	app, err := initHeadlessApp(ctx)
	if err != nil {
		cliError("init failed: %v", err)
		return 1
	}
	defer app.ShutdownHeadless()

	connID, _ := app.store.GetSetting("cli_active_connection")
	if connID == "" {
		cliJSON(ConnectionStatus{Connected: false})
		return 0
	}

	// Try to reconnect and ping
	if err := app.ReconnectFromStore(connID); err != nil {
		cliJSON(ConnectionStatus{Connected: false, ID: connID})
		return 0
	}
	defer app.Disconnect()

	cliJSON(ConnectionStatus{
		Connected:  true,
		ID:         app.connID,
		Name:       app.connName,
		DriverType: app.connType,
	})
	return 0
}

func cmdQuery(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("query", flag.ContinueOnError)
	sqlFlag := fs.String("sql", "", "SQL query")
	fileFlag := fs.String("file", "", "Path to SQL file")
	limit := fs.Int("limit", 10000, "Row limit")

	if err := fs.Parse(args); err != nil {
		cliError("invalid flags: %v", err)
		return 1
	}

	sql := *sqlFlag
	if sql == "" && fs.NArg() > 0 {
		sql = fs.Arg(0)
	}
	if sql == "" && *fileFlag != "" {
		data, err := os.ReadFile(*fileFlag)
		if err != nil {
			cliError("read file: %v", err)
			return 1
		}
		sql = string(data)
	}
	if sql == "" {
		cliError("SQL query required (positional arg, --sql, or --file)")
		return 1
	}

	app, err := initHeadlessApp(ctx)
	if err != nil {
		cliError("init failed: %v", err)
		return 1
	}
	defer app.ShutdownHeadless()

	if err := reconnectActiveOrEnv(app); err != nil {
		cliError("%v", err)
		return 1
	}
	defer app.Disconnect()

	result, err := app.Execute(sql, *limit)
	if err != nil {
		cliError("query failed: %v", err)
		return 1
	}

	cliJSON(result)
	return 0
}

func cmdSchema(ctx context.Context, args []string) int {
	if len(args) == 0 {
		cliError("usage: warehouse-ui schema <list-databases|list-tables|describe> [flags]")
		return 1
	}

	subcmd := args[0]
	rest := args[1:]

	fs := flag.NewFlagSet("schema "+subcmd, flag.ContinueOnError)
	database := fs.String("database", "", "Database name")
	if err := fs.Parse(rest); err != nil {
		cliError("invalid flags: %v", err)
		return 1
	}

	app, err := initHeadlessApp(ctx)
	if err != nil {
		cliError("init failed: %v", err)
		return 1
	}
	defer app.ShutdownHeadless()

	if err := reconnectActiveOrEnv(app); err != nil {
		cliError("%v", err)
		return 1
	}
	defer app.Disconnect()

	switch subcmd {
	case "list-databases":
		dbs, err := app.ListDatabases()
		if err != nil {
			cliError("list databases: %v", err)
			return 1
		}
		cliJSON(dbs)

	case "list-tables":
		tables, err := app.ListTables(*database)
		if err != nil {
			cliError("list tables: %v", err)
			return 1
		}
		cliJSON(tables)

	case "describe":
		tableName := fs.Arg(0)
		if tableName == "" {
			cliError("table name required: warehouse-ui schema describe <table>")
			return 1
		}
		schema, err := app.GetTableSchema(*database, tableName)
		if err != nil {
			cliError("describe table: %v", err)
			return 1
		}
		cliJSON(schema)

	default:
		cliError("unknown schema command: %s (use list-databases, list-tables, describe)", subcmd)
		return 1
	}

	return 0
}

func cmdDryRun(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("dry-run", flag.ContinueOnError)
	sqlFlag := fs.String("sql", "", "SQL query")

	if err := fs.Parse(args); err != nil {
		cliError("invalid flags: %v", err)
		return 1
	}

	sql := *sqlFlag
	if sql == "" && fs.NArg() > 0 {
		sql = fs.Arg(0)
	}
	if sql == "" {
		cliError("SQL query required")
		return 1
	}

	app, err := initHeadlessApp(ctx)
	if err != nil {
		cliError("init failed: %v", err)
		return 1
	}
	defer app.ShutdownHeadless()

	if err := reconnectActiveOrEnv(app); err != nil {
		cliError("%v", err)
		return 1
	}
	defer app.Disconnect()

	result, err := app.DryRun(sql)
	if err != nil {
		cliError("dry run failed: %v", err)
		return 1
	}

	cliJSON(result)
	return 0
}

func cmdAI(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("ai", flag.ContinueOnError)
	execute := fs.Bool("execute", false, "Execute the generated SQL after generation")

	if err := fs.Parse(args); err != nil {
		cliError("invalid flags: %v", err)
		return 1
	}

	prompt := strings.Join(fs.Args(), " ")
	if prompt == "" {
		cliError("prompt required: warehouse-ui ai \"your question here\"")
		return 1
	}

	app, err := initHeadlessApp(ctx)
	if err != nil {
		cliError("init failed: %v", err)
		return 1
	}
	defer app.ShutdownHeadless()

	if err := reconnectActiveOrEnv(app); err != nil {
		cliError("%v", err)
		return 1
	}
	defer app.Disconnect()

	// Generate SQL from natural language
	sql, err := app.AiGenerateSQL(prompt)
	if err != nil {
		cliError("AI generation failed: %v", err)
		return 1
	}

	result := map[string]interface{}{
		"sql": sql,
	}

	if *execute {
		qr, err := app.Execute(sql, 10000)
		if err != nil {
			cliError("query execution failed: %v", err)
			return 1
		}
		result["result"] = qr
	}

	cliJSON(result)
	return 0
}

func cmdConnections(ctx context.Context, args []string) int {
	app, err := initHeadlessApp(ctx)
	if err != nil {
		cliError("init failed: %v", err)
		return 1
	}
	defer app.ShutdownHeadless()

	conns, err := app.ListSavedConnections()
	if err != nil {
		cliError("list connections: %v", err)
		return 1
	}

	activeID, _ := app.store.GetSetting("cli_active_connection")

	if outputFormat == "table" {
		w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "ACTIVE\tID\tNAME\tTYPE\tCREATED")
		fmt.Fprintln(w, "------\t--\t----\t----\t-------")
		for _, c := range conns {
			active := ""
			if c.ID == activeID {
				active = "*"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", active, c.ID[:8], c.Name, c.DriverType, c.CreatedAt)
		}
		w.Flush()
		return 0
	}

	type connInfo struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		DriverType string `json:"driver_type"`
		Active     bool   `json:"active"`
		CreatedAt  string `json:"created_at"`
	}
	var out []connInfo
	for _, c := range conns {
		out = append(out, connInfo{
			ID:         c.ID,
			Name:       c.Name,
			DriverType: c.DriverType,
			Active:     c.ID == activeID,
			CreatedAt:  c.CreatedAt,
		})
	}
	cliJSON(out)
	return 0
}

func cmdHistory(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("history", flag.ContinueOnError)
	search := fs.String("search", "", "Filter by SQL text")
	limit := fs.Int("limit", 20, "Number of entries")

	if err := fs.Parse(args); err != nil {
		cliError("invalid flags: %v", err)
		return 1
	}

	app, err := initHeadlessApp(ctx)
	if err != nil {
		cliError("init failed: %v", err)
		return 1
	}
	defer app.ShutdownHeadless()

	entries, err := app.GetHistory(*search, *limit, 0)
	if err != nil {
		cliError("get history: %v", err)
		return 1
	}

	if outputFormat == "table" {
		w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "STATUS\tDRIVER\tDURATION\tSQL")
		fmt.Fprintln(w, "------\t------\t--------\t---")
		for _, e := range entries {
			dur := ""
			if e.DurationMs != nil {
				dur = formatDurationMs(float64(*e.DurationMs))
			}
			sql := e.SQL
			if len(sql) > 80 {
				sql = sql[:77] + "..."
			}
			sql = strings.ReplaceAll(sql, "\n", " ")
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", e.Status, e.DriverType, dur, sql)
		}
		w.Flush()
		return 0
	}

	cliJSON(entries)
	return 0
}

func cmdVersion() int {
	cliJSON(map[string]string{
		"name":    "warehouse-ui",
		"version": Version,
	})
	return 0
}
