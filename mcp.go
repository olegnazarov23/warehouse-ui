package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// MCP JSON-RPC types
type mcpRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type mcpResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *mcpError   `json:"error,omitempty"`
}

type mcpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type mcpContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type mcpToolResult struct {
	Content []mcpContent `json:"content"`
	IsError bool         `json:"isError"`
}

type mcpTool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema interface{} `json:"inputSchema"`
}

type mcpToolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func mcpWrite(resp mcpResponse) {
	b, _ := json.Marshal(resp)
	fmt.Fprintf(os.Stdout, "%s\n", b)
}

func mcpToolError(id json.RawMessage, msg string) {
	mcpWrite(mcpResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result: mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: msg}},
			IsError: true,
		},
	})
}

func mcpToolSuccess(id json.RawMessage, text string) {
	mcpWrite(mcpResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result: mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: text}},
			IsError: false,
		},
	})
}

func mcpRPCError(id json.RawMessage, code int, msg string) {
	mcpWrite(mcpResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &mcpError{Code: code, Message: msg},
	})
}

// mcpTools returns the list of tools exposed by the MCP server.
func mcpTools() []mcpTool {
	return []mcpTool{
		{
			Name:        "connect",
			Description: "Connect to a database using a connection URL or saved connection ID",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"url":           map[string]interface{}{"type": "string", "description": "Database connection URL (e.g. postgres://user:pass@host/db)"},
					"connection_id": map[string]interface{}{"type": "string", "description": "ID of a saved connection to reconnect to"},
				},
			},
		},
		{
			Name:        "disconnect",
			Description: "Disconnect from the current database",
			InputSchema: map[string]interface{}{"type": "object"},
		},
		{
			Name:        "status",
			Description: "Show current connection status",
			InputSchema: map[string]interface{}{"type": "object"},
		},
		{
			Name:        "execute_query",
			Description: "Execute a SQL query against the connected database and return results as JSON",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"sql":   map[string]interface{}{"type": "string", "description": "SQL query to execute"},
					"limit": map[string]interface{}{"type": "integer", "description": "Max rows to return (default 1000)"},
				},
				"required": []string{"sql"},
			},
		},
		{
			Name:        "list_databases",
			Description: "List all databases on the connected server",
			InputSchema: map[string]interface{}{"type": "object"},
		},
		{
			Name:        "list_tables",
			Description: "List all tables in a database with row counts and column counts",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"database": map[string]interface{}{"type": "string", "description": "Database name (optional for single-database drivers)"},
				},
			},
		},
		{
			Name:        "describe_table",
			Description: "Get detailed schema for a table including columns, types, and nullability",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"table":    map[string]interface{}{"type": "string", "description": "Table name"},
					"database": map[string]interface{}{"type": "string", "description": "Database name (optional)"},
				},
				"required": []string{"table"},
			},
		},
		{
			Name:        "dry_run",
			Description: "Validate SQL and estimate query cost (bytes, cost for BigQuery; row estimates for PostgreSQL/MySQL)",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"sql": map[string]interface{}{"type": "string", "description": "SQL query to analyze"},
				},
				"required": []string{"sql"},
			},
		},
		{
			Name:        "list_connections",
			Description: "List all saved database connections",
			InputSchema: map[string]interface{}{"type": "object"},
		},
		{
			Name:        "query_history",
			Description: "Show recent query execution history",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"search": map[string]interface{}{"type": "string", "description": "Filter by SQL text"},
					"limit":  map[string]interface{}{"type": "integer", "description": "Number of entries (default 20)"},
				},
			},
		},
	}
}

func cmdMCP(ctx context.Context) int {
	app, err := initHeadlessApp(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "MCP init failed: %v\n", err)
		return 1
	}
	defer app.ShutdownHeadless()

	// Auto-connect from DATABASE_URL if set
	if dbURL := os.Getenv("DATABASE_URL"); dbURL != "" {
		cfg, err := app.ParseConnectionString(dbURL, "")
		if err == nil {
			cfg.ID = "env-database-url"
			cfg.Name = "DATABASE_URL"
			_, _ = app.Connect(*cfg)
		}
	}

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4*1024*1024), 4*1024*1024) // 4MB buffer

	fmt.Fprintf(os.Stderr, "warehouse-ui MCP server started (version %s)\n", Version)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req mcpRequest
		if err := json.Unmarshal(line, &req); err != nil {
			mcpRPCError(nil, -32700, "Parse error: "+err.Error())
			continue
		}

		// Notifications (no id) — don't respond
		if req.ID == nil || string(req.ID) == "null" {
			continue
		}

		switch req.Method {
		case "initialize":
			mcpWrite(mcpResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result: map[string]interface{}{
					"protocolVersion": "2025-11-25",
					"capabilities": map[string]interface{}{
						"tools": map[string]interface{}{"listChanged": false},
					},
					"serverInfo": map[string]interface{}{
						"name":    "warehouse-ui",
						"version": Version,
					},
				},
			})

		case "tools/list":
			mcpWrite(mcpResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  map[string]interface{}{"tools": mcpTools()},
			})

		case "tools/call":
			var params mcpToolCallParams
			if err := json.Unmarshal(req.Params, &params); err != nil {
				mcpRPCError(req.ID, -32602, "Invalid params: "+err.Error())
				continue
			}
			handleMCPToolCall(ctx, app, req.ID, params)

		case "ping":
			mcpWrite(mcpResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  map[string]interface{}{},
			})

		default:
			mcpRPCError(req.ID, -32601, "Method not found: "+req.Method)
		}
	}

	return 0
}

func handleMCPToolCall(ctx context.Context, app *App, id json.RawMessage, params mcpToolCallParams) {
	var args map[string]interface{}
	if len(params.Arguments) > 0 {
		_ = json.Unmarshal(params.Arguments, &args)
	}
	if args == nil {
		args = make(map[string]interface{})
	}

	getString := func(key string) string {
		if v, ok := args[key]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
		return ""
	}
	getInt := func(key string, def int) int {
		if v, ok := args[key]; ok {
			if f, ok := v.(float64); ok {
				return int(f)
			}
		}
		return def
	}

	toJSON := func(v interface{}) string {
		b, _ := json.MarshalIndent(v, "", "  ")
		return string(b)
	}

	switch params.Name {
	case "connect":
		url := getString("url")
		connID := getString("connection_id")
		if connID != "" {
			if err := app.ReconnectFromStore(connID); err != nil {
				mcpToolError(id, "Connect failed: "+err.Error())
				return
			}
			mcpToolSuccess(id, fmt.Sprintf("Connected to saved connection %s", connID))
			return
		}
		if url == "" {
			mcpToolError(id, "Either 'url' or 'connection_id' is required")
			return
		}
		cfg, err := app.ParseConnectionString(url, "")
		if err != nil {
			mcpToolError(id, "Invalid URL: "+err.Error())
			return
		}
		cfg.ID = "mcp-connection"
		cfg.Name = "MCP"
		status, err := app.Connect(*cfg)
		if err != nil {
			mcpToolError(id, "Connect failed: "+err.Error())
			return
		}
		mcpToolSuccess(id, toJSON(status))

	case "disconnect":
		_ = app.Disconnect()
		mcpToolSuccess(id, "Disconnected")

	case "status":
		if app.driver == nil {
			mcpToolSuccess(id, toJSON(ConnectionStatus{Connected: false}))
			return
		}
		mcpToolSuccess(id, toJSON(ConnectionStatus{
			Connected:  true,
			ID:         app.connID,
			Name:       app.connName,
			DriverType: app.connType,
		}))

	case "execute_query":
		sql := getString("sql")
		if sql == "" {
			mcpToolError(id, "Parameter 'sql' is required")
			return
		}
		limit := getInt("limit", 1000)
		if app.driver == nil {
			mcpToolError(id, "Not connected — use the 'connect' tool first")
			return
		}
		result, err := app.Execute(sql, limit)
		if err != nil {
			mcpToolError(id, "Query failed: "+err.Error())
			return
		}
		mcpToolSuccess(id, toJSON(result))

	case "list_databases":
		if app.driver == nil {
			mcpToolError(id, "Not connected")
			return
		}
		dbs, err := app.ListDatabases()
		if err != nil {
			mcpToolError(id, "List databases failed: "+err.Error())
			return
		}
		mcpToolSuccess(id, toJSON(dbs))

	case "list_tables":
		if app.driver == nil {
			mcpToolError(id, "Not connected")
			return
		}
		database := getString("database")
		tables, err := app.ListTables(database)
		if err != nil {
			mcpToolError(id, "List tables failed: "+err.Error())
			return
		}
		// Summarize for the LLM
		var sb strings.Builder
		for _, t := range tables {
			sb.WriteString(fmt.Sprintf("- %s (%d rows, %d columns)\n", t.Name, t.RowCount, len(t.Columns)))
		}
		mcpToolSuccess(id, sb.String())

	case "describe_table":
		if app.driver == nil {
			mcpToolError(id, "Not connected")
			return
		}
		table := getString("table")
		if table == "" {
			mcpToolError(id, "Parameter 'table' is required")
			return
		}
		database := getString("database")
		schema, err := app.GetTableSchema(database, table)
		if err != nil {
			mcpToolError(id, "Describe table failed: "+err.Error())
			return
		}
		mcpToolSuccess(id, toJSON(schema))

	case "dry_run":
		if app.driver == nil {
			mcpToolError(id, "Not connected")
			return
		}
		sql := getString("sql")
		if sql == "" {
			mcpToolError(id, "Parameter 'sql' is required")
			return
		}
		result, err := app.DryRun(sql)
		if err != nil {
			mcpToolError(id, "Dry run failed: "+err.Error())
			return
		}
		mcpToolSuccess(id, toJSON(result))

	case "list_connections":
		conns, err := app.ListSavedConnections()
		if err != nil {
			mcpToolError(id, "List connections failed: "+err.Error())
			return
		}
		type connSummary struct {
			ID         string `json:"id"`
			Name       string `json:"name"`
			DriverType string `json:"driver_type"`
		}
		var out []connSummary
		for _, c := range conns {
			out = append(out, connSummary{ID: c.ID, Name: c.Name, DriverType: c.DriverType})
		}
		mcpToolSuccess(id, toJSON(out))

	case "query_history":
		search := getString("search")
		limit := getInt("limit", 20)
		entries, err := app.GetHistory(search, limit, 0)
		if err != nil {
			mcpToolError(id, "Get history failed: "+err.Error())
			return
		}
		mcpToolSuccess(id, toJSON(entries))

	default:
		mcpRPCError(id, -32602, "Unknown tool: "+params.Name)
	}
}
