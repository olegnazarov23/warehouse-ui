package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/google/uuid"
	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"warehouse-ui/internal/ai"
	"warehouse-ui/internal/driver"
	"warehouse-ui/internal/logger"
	"warehouse-ui/internal/store"
	"warehouse-ui/internal/tunnel"
)

// queryCacheEntry holds a cached query result with expiry.
type queryCacheEntry struct {
	result    *driver.QueryResult
	cachedAt  time.Time
	sql       string
	connID    string
}

const (
	queryCacheTTL     = 5 * time.Minute
	queryCacheMaxSize = 50
)

// App is the main application struct. All public methods are bound to the
// frontend via Wails and become callable from JavaScript/Svelte.
type App struct {
	ctx context.Context

	store  *store.Store
	driver driver.Driver
	aiProv ai.Provider

	// Current connection info
	connID   string
	connName string
	connType driver.DriverType

	// SSH tunnel (if active)
	sshTunnel *tunnel.Tunnel

	// Code repository paths for AI context
	codePaths []string

	// Query result cache: key = sha256(connID + sql + limit)
	queryCache map[string]*queryCacheEntry
	cacheMu    sync.RWMutex

	mu sync.RWMutex
}

// NewApp creates a new App instance.
func NewApp() *App {
	return &App{}
}

// startup is called by Wails when the app starts.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	// Initialize the local store
	dataDir := filepath.Join(userDataDir(), "warehouse-ui")

	// Initialize logger first
	logger.Init(dataDir)
	logger.Info("starting up, data dir: %s", dataDir)

	s, err := store.New(dataDir)
	if err != nil {
		logger.Error("failed to init store: %v", err)
		return
	}
	a.store = s

	// Load AI settings from store
	a.loadAISettings()
	logger.Info("startup complete")
}

// shutdown is called when the app is closing.
func (a *App) shutdown(ctx context.Context) {
	logger.Info("shutting down")
	if a.driver != nil {
		a.driver.Disconnect()
	}
	if a.store != nil {
		a.store.Close()
	}
	logger.Close()
}

// =========================================================================
// Connection Management
// =========================================================================

// ConnectionStatus describes the current connection state.
type ConnectionStatus struct {
	Connected  bool              `json:"connected"`
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	DriverType driver.DriverType `json:"driver_type"`
	Database   string            `json:"database"`
}

// Connect establishes a connection to a database.
func (a *App) Connect(cfg driver.ConnectionConfig) (ConnectionStatus, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Disconnect existing
	if a.driver != nil {
		a.driver.Disconnect()
		a.driver = nil
	}
	if a.sshTunnel != nil {
		a.sshTunnel.Close()
		a.sshTunnel = nil
	}

	logger.Info("connecting: driver=%s host=%s db=%s", cfg.Type, cfg.Host, cfg.Database)

	// Set up SSH tunnel if configured
	if sshHost := cfg.Options["ssh_host"]; sshHost != "" {
		remoteHost, remotePort := splitHostPort(cfg.Host, defaultPort(cfg.Type))

		sshCfg := tunnel.SSHConfig{
			Host:       sshHost,
			User:       cfg.Options["ssh_user"],
			Password:   cfg.Options["ssh_password"],
			KeyPath:    cfg.Options["ssh_key_path"],
			JumpHost:   cfg.Options["ssh_jump_host"],
			RemoteHost: remoteHost,
			RemotePort: remotePort,
		}

		tun, err := tunnel.Open(a.ctx, sshCfg)
		if err != nil {
			logger.Error("ssh tunnel failed: %v", err)
			return ConnectionStatus{}, fmt.Errorf("SSH tunnel: %w", err)
		}
		a.sshTunnel = tun

		// Rewrite host to tunnel's local address
		cfg.Host = tun.LocalAddr()
		logger.Info("ssh tunnel established: %s -> %s:%s", tun.LocalAddr(), remoteHost, remotePort)
	}

	d, err := driver.New(cfg.Type)
	if err != nil {
		logger.Error("driver init failed: %v", err)
		return ConnectionStatus{}, err
	}

	if err := d.Connect(a.ctx, cfg); err != nil {
		logger.Error("connect failed: %v", err)
		if a.sshTunnel != nil {
			a.sshTunnel.Close()
			a.sshTunnel = nil
		}
		return ConnectionStatus{}, err
	}

	logger.Info("connected successfully: %s", cfg.Type)
	a.driver = d
	a.connID = cfg.ID
	a.connName = cfg.Name
	a.connType = cfg.Type

	// Load code paths from connection options
	if cp, ok := cfg.Options["code_paths"]; ok && cp != "" {
		var paths []string
		if json.Unmarshal([]byte(cp), &paths) == nil {
			a.codePaths = paths
		}
	} else {
		a.codePaths = nil
	}

	return ConnectionStatus{
		Connected:  true,
		ID:         cfg.ID,
		Name:       cfg.Name,
		DriverType: cfg.Type,
		Database:   cfg.Database,
	}, nil
}

// Disconnect closes the current database connection.
func (a *App) Disconnect() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Clear query cache on disconnect
	a.cacheMu.Lock()
	a.queryCache = make(map[string]*queryCacheEntry)
	a.cacheMu.Unlock()

	if a.driver != nil {
		err := a.driver.Disconnect()
		a.driver = nil
		a.connID = ""
		a.connName = ""
		if a.sshTunnel != nil {
			a.sshTunnel.Close()
			a.sshTunnel = nil
		}
		return err
	}
	return nil
}

// GetConnectionStatus returns the current connection state.
func (a *App) GetConnectionStatus() ConnectionStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()

	return ConnectionStatus{
		Connected:  a.driver != nil,
		ID:         a.connID,
		Name:       a.connName,
		DriverType: a.connType,
	}
}

// GetAvailableDrivers returns all supported database types.
func (a *App) GetAvailableDrivers() []string {
	types := driver.AvailableDrivers()
	out := make([]string, len(types))
	for i, t := range types {
		out[i] = string(t)
	}
	return out
}

// PickFile opens a native file dialog and returns the selected path.
func (a *App) PickFile(title string) string {
	path, err := wailsRuntime.OpenFileDialog(a.ctx, wailsRuntime.OpenDialogOptions{
		Title: title,
		Filters: []wailsRuntime.FileFilter{
			{DisplayName: "JSON Files", Pattern: "*.json"},
			{DisplayName: "SQLite Database", Pattern: "*.db;*.sqlite;*.sqlite3"},
			{DisplayName: "All Files", Pattern: "*"},
		},
	})
	if err != nil {
		return ""
	}
	return path
}

// PickFolder opens a native directory dialog and returns the selected path.
func (a *App) PickFolder(title string) string {
	path, err := wailsRuntime.OpenDirectoryDialog(a.ctx, wailsRuntime.OpenDialogOptions{
		Title: title,
	})
	if err != nil {
		return ""
	}
	return path
}

// SetCodePaths sets the code repository paths for AI context.
func (a *App) SetCodePaths(paths []string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.codePaths = paths
}

// GetCodePaths returns the current code repository paths.
func (a *App) GetCodePaths() []string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.codePaths
}

// =========================================================================
// Code Context Scanning
// =========================================================================

// CodeSnippet represents a relevant code fragment from a repo.
type CodeSnippet struct {
	FilePath string `json:"file_path"` // relative to repo root
	Language string `json:"language"`  // go, python, typescript, sql, etc.
	Content  string `json:"content"`   // the relevant code
	LineNum  int    `json:"line_num"`  // starting line number
}

// DetectedConnection is a database connection found in code (env files, config, connection strings).
type DetectedConnection struct {
	Source     string `json:"source"`      // file where it was found
	DriverHint string `json:"driver_hint"` // postgres, mysql, bigquery, etc.
	Detail     string `json:"detail"`      // connection string or config snippet
}

// CodeContext holds extracted code snippets from user repos.
type CodeContext struct {
	Paths               []string              `json:"paths"`
	Snippets            []CodeSnippet         `json:"snippets"`
	DetectedConnections []DetectedConnection  `json:"detected_connections"`
	Summary             string                `json:"summary"` // brief stats
}

// ScanCodeContext walks one or more local folders and extracts SQL-related code
// snippets: SQL files, query strings in Go/Python/JS/TS, ORM models, migrations.
// Returns a compact context suitable for AI prompt injection.
func (a *App) ScanCodeContext(paths []string) (*CodeContext, error) {
	result := &CodeContext{Paths: paths}

	// File extensions to scan for SQL patterns
	sqlExts := map[string]string{
		".sql": "sql", ".go": "go", ".py": "python",
		".ts": "typescript", ".tsx": "typescript", ".js": "javascript",
		".jsx": "javascript", ".rb": "ruby", ".java": "java",
		".cs": "csharp", ".rs": "rust", ".php": "php",
	}

	// Config/env files to scan for connection strings
	configFiles := map[string]bool{
		".env": true, ".env.local": true, ".env.example": true,
		".env.development": true, ".env.production": true,
		"database.yml": true, "database.yaml": true,
		"docker-compose.yml": true, "docker-compose.yaml": true,
	}

	// Connection string patterns → driver hint
	connPatterns := []struct {
		pattern string
		driver  string
	}{
		{"postgres://", "postgres"}, {"postgresql://", "postgres"},
		{"mysql://", "mysql"}, {"mongodb://", "mongodb"}, {"mongodb+srv://", "mongodb"},
		{"clickhouse://", "clickhouse"}, {"sqlite:", "sqlite"},
		{"bigquery", "bigquery"}, {"GOOGLE_APPLICATION_CREDENTIALS", "bigquery"},
		{"DATABASE_URL", ""}, {"DB_HOST", ""}, {"DB_CONNECTION", ""},
		{"PGHOST", "postgres"}, {"PGDATABASE", "postgres"},
		{"MYSQL_HOST", "mysql"}, {"MYSQL_DATABASE", "mysql"},
		{"REDIS_URL", "redis"}, {"REDIS_HOST", "redis"},
		{"ELASTICSEARCH_URL", "elasticsearch"},
		{"MONGO_URI", "mongodb"}, {"MONGODB_URI", "mongodb"},
	}

	// Patterns that indicate SQL-related code
	sqlPatterns := []string{
		"SELECT ", "INSERT ", "UPDATE ", "DELETE ", "CREATE TABLE",
		"ALTER TABLE", "JOIN ", "FROM ", "WHERE ",
		"query(", "execute(", "sql.Open", "sqlx.", "gorm.",
		"sequelize", "prisma", "typeorm", "knex",
		"SQLAlchemy", "django.db", "ActiveRecord",
		"migration", "Migration",
	}

	const maxSnippets = 50
	const maxFileSize = 100 * 1024 // 100KB per file
	const contextLines = 5         // lines before/after match

	for _, root := range paths {
		if len(result.Snippets) >= maxSnippets {
			break
		}

		filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil // skip errors
			}
			if len(result.Snippets) >= maxSnippets {
				return filepath.SkipAll
			}

			// Skip hidden dirs, node_modules, vendor, etc.
			if info.IsDir() {
				name := info.Name()
				if strings.HasPrefix(name, ".") || name == "node_modules" ||
					name == "vendor" || name == "__pycache__" || name == "dist" ||
					name == "build" || name == "target" || name == ".git" {
					return filepath.SkipDir
				}
				return nil
			}

			// Check for config/env files with connection strings
			fileName := info.Name()
			if configFiles[fileName] && info.Size() < int64(maxFileSize) {
				data, err := os.ReadFile(path)
				if err == nil {
					content := string(data)
					for _, cp := range connPatterns {
						if strings.Contains(content, cp.pattern) {
							relPath, _ := filepath.Rel(root, path)
							// Extract the line containing the pattern
							for _, line := range strings.Split(content, "\n") {
								if strings.Contains(line, cp.pattern) {
									line = strings.TrimSpace(line)
									// Mask actual passwords/keys
									if strings.Contains(line, "=") {
										parts := strings.SplitN(line, "=", 2)
										if len(parts) == 2 {
											val := strings.TrimSpace(parts[1])
											if len(val) > 20 {
												line = parts[0] + "=" + val[:8] + "..." + val[len(val)-4:]
											}
										}
									}
									result.DetectedConnections = append(result.DetectedConnections, DetectedConnection{
										Source:     relPath,
										DriverHint: cp.driver,
										Detail:     line,
									})
									break
								}
							}
						}
					}
				}
				// Also return — config files aren't code files
				return nil
			}

			// Check extension
			ext := strings.ToLower(filepath.Ext(path))
			lang, ok := sqlExts[ext]
			if !ok {
				return nil
			}

			// Skip large files
			if info.Size() > int64(maxFileSize) {
				return nil
			}

			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			content := string(data)
			lines := strings.Split(content, "\n")

			// For .sql files, include the whole file (truncated)
			if ext == ".sql" {
				snippet := content
				if len(snippet) > 2000 {
					snippet = snippet[:2000] + "\n-- ... (truncated)"
				}
				relPath, _ := filepath.Rel(root, path)
				result.Snippets = append(result.Snippets, CodeSnippet{
					FilePath: relPath,
					Language: lang,
					Content:  snippet,
					LineNum:  1,
				})
				return nil
			}

			// For code files, find lines with SQL patterns and extract context
			for lineIdx, line := range lines {
				if len(result.Snippets) >= maxSnippets {
					break
				}

				upperLine := strings.ToUpper(line)
				matched := false
				for _, pat := range sqlPatterns {
					if strings.Contains(upperLine, strings.ToUpper(pat)) {
						matched = true
						break
					}
				}
				if !matched {
					continue
				}

				// Extract surrounding context
				start := lineIdx - contextLines
				if start < 0 {
					start = 0
				}
				end := lineIdx + contextLines + 1
				if end > len(lines) {
					end = len(lines)
				}

				snippet := strings.Join(lines[start:end], "\n")
				if len(snippet) > 1500 {
					snippet = snippet[:1500] + "\n// ... (truncated)"
				}

				relPath, _ := filepath.Rel(root, path)
				result.Snippets = append(result.Snippets, CodeSnippet{
					FilePath: relPath,
					Language: lang,
					Content:  snippet,
					LineNum:  start + 1,
				})

				// Skip ahead to avoid overlapping snippets from the same region
				lineIdx += contextLines * 2
			}

			return nil
		})
	}

	// Also scan docker-compose files for service databases
	for _, root := range paths {
		for _, dcFile := range []string{"docker-compose.yml", "docker-compose.yaml", "compose.yml", "compose.yaml"} {
			dcPath := filepath.Join(root, dcFile)
			data, err := os.ReadFile(dcPath)
			if err != nil {
				continue
			}
			relPath, _ := filepath.Rel(root, dcPath)
			content := string(data)
			// Simple YAML parsing for common database images
			dockerDBs := []struct {
				image  string
				driver string
				port   string
			}{
				{"postgres", "postgres", "5432"},
				{"mysql", "mysql", "3306"},
				{"mariadb", "mysql", "3306"},
				{"mongo", "mongodb", "27017"},
				{"redis", "redis", "6379"},
				{"clickhouse", "clickhouse", "9000"},
			}
			for _, db := range dockerDBs {
				if strings.Contains(content, "image: "+db.image) || strings.Contains(content, "image: '"+db.image) || strings.Contains(content, "image: \""+db.image) {
					// Try to extract port mapping and env vars
					detail := fmt.Sprintf("Docker %s (localhost:%s)", db.image, db.port)
					result.DetectedConnections = append(result.DetectedConnections, DetectedConnection{
						Source:     relPath,
						DriverHint: db.driver,
						Detail:     detail,
					})
				}
			}
		}
	}

	if len(result.DetectedConnections) > 0 {
		result.Summary = fmt.Sprintf("Scanned %d repos, found %d SQL-related code snippets and %d database connections", len(paths), len(result.Snippets), len(result.DetectedConnections))
	} else {
		result.Summary = fmt.Sprintf("Scanned %d repos, found %d SQL-related code snippets", len(paths), len(result.Snippets))
	}
	return result, nil
}

// ParseConnectionString parses a database connection URI or env var into a ConnectionConfig.
// Supports: postgres://, mysql://, mongodb://, clickhouse://, and key=value env vars.
func (a *App) ParseConnectionString(connStr, driverHint string) (*driver.ConnectionConfig, error) {
	connStr = strings.TrimSpace(connStr)

	// Handle env var format: KEY=value → extract value
	if strings.Contains(connStr, "=") && !strings.Contains(connStr, "://") {
		parts := strings.SplitN(connStr, "=", 2)
		if len(parts) == 2 {
			connStr = strings.TrimSpace(parts[1])
			connStr = strings.Trim(connStr, "'\"")
		}
	}

	cfg := &driver.ConnectionConfig{
		ID:      uuid.New().String(),
		Options: make(map[string]string),
		SSLMode: "disable",
	}

	// Try URL parsing for standard connection strings
	if strings.Contains(connStr, "://") {
		u, err := url.Parse(connStr)
		if err != nil {
			return nil, fmt.Errorf("invalid connection string: %w", err)
		}

		// Detect driver from scheme
		switch {
		case strings.HasPrefix(u.Scheme, "postgres"):
			cfg.Type = driver.Postgres
			cfg.Host = u.Host
			if u.Host == "" {
				cfg.Host = "localhost:5432"
			} else if !strings.Contains(u.Host, ":") {
				cfg.Host = u.Host + ":5432"
			}
			cfg.Database = strings.TrimPrefix(u.Path, "/")
			if u.User != nil {
				cfg.Username = u.User.Username()
				cfg.Password, _ = u.User.Password()
			}
			if sslMode := u.Query().Get("sslmode"); sslMode != "" {
				cfg.SSLMode = sslMode
			}
		case u.Scheme == "mysql":
			cfg.Type = driver.MySQL
			cfg.Host = u.Host
			if u.Host == "" {
				cfg.Host = "localhost:3306"
			} else if !strings.Contains(u.Host, ":") {
				cfg.Host = u.Host + ":3306"
			}
			cfg.Database = strings.TrimPrefix(u.Path, "/")
			if u.User != nil {
				cfg.Username = u.User.Username()
				cfg.Password, _ = u.User.Password()
			}
		case strings.HasPrefix(u.Scheme, "mongodb"):
			cfg.Type = driver.MongoDB
			cfg.Host = u.Host
			if u.Host == "" {
				cfg.Host = "localhost:27017"
			}
			cfg.Database = strings.TrimPrefix(u.Path, "/")
			if u.User != nil {
				cfg.Username = u.User.Username()
				cfg.Password, _ = u.User.Password()
			}
			cfg.Options["connection_string"] = connStr
		case u.Scheme == "clickhouse":
			cfg.Type = driver.ClickHouse
			cfg.Host = u.Host
			if u.Host == "" {
				cfg.Host = "localhost:9000"
			}
			cfg.Database = strings.TrimPrefix(u.Path, "/")
			if u.User != nil {
				cfg.Username = u.User.Username()
				cfg.Password, _ = u.User.Password()
			}
		default:
			return nil, fmt.Errorf("unsupported scheme: %s", u.Scheme)
		}

		cfg.Name = fmt.Sprintf("%s — %s", cfg.Type, cfg.Database)
		if cfg.Database == "" {
			cfg.Name = fmt.Sprintf("%s — %s", cfg.Type, cfg.Host)
		}
		return cfg, nil
	}

	// Handle Docker-style descriptions: "Docker postgres (localhost:5432)"
	dockerRe := regexp.MustCompile(`Docker\s+(\w+)\s+\(([^)]+)\)`)
	if m := dockerRe.FindStringSubmatch(connStr); len(m) == 3 {
		image := m[1]
		host := m[2]

		switch {
		case strings.Contains(image, "postgres"):
			cfg.Type = driver.Postgres
			cfg.Username = "postgres"
			cfg.Password = "postgres"
			cfg.Database = "postgres"
		case strings.Contains(image, "mysql") || strings.Contains(image, "mariadb"):
			cfg.Type = driver.MySQL
			cfg.Username = "root"
			cfg.Password = "root"
			cfg.Database = "mysql"
		case strings.Contains(image, "mongo"):
			cfg.Type = driver.MongoDB
			cfg.Database = "test"
		case strings.Contains(image, "clickhouse"):
			cfg.Type = driver.ClickHouse
			cfg.Username = "default"
			cfg.Database = "default"
		default:
			if driverHint != "" {
				cfg.Type = driver.DriverType(driverHint)
			} else {
				return nil, fmt.Errorf("cannot determine driver for: %s", image)
			}
		}

		cfg.Host = host
		cfg.Name = fmt.Sprintf("Docker %s — %s", image, host)
		return cfg, nil
	}

	// Fallback: use driver hint
	if driverHint != "" {
		cfg.Type = driver.DriverType(driverHint)
		cfg.Name = fmt.Sprintf("%s (from code)", driverHint)
		return cfg, nil
	}

	return nil, fmt.Errorf("could not parse connection string: %s", connStr)
}

// TestConnection attempts to connect to a database and immediately disconnects.
// Returns nil on success, or an error describing the failure.
func (a *App) TestConnection(cfg driver.ConnectionConfig) error {
	d, err := driver.New(cfg.Type)
	if err != nil {
		return fmt.Errorf("unsupported driver %s: %w", cfg.Type, err)
	}

	ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
	defer cancel()

	if err := d.Connect(ctx, cfg); err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}
	defer d.Disconnect()

	if err := d.Ping(ctx); err != nil {
		return fmt.Errorf("ping failed: %w", err)
	}

	return nil
}

// ListServerDatabases temporarily connects to a server and returns available database names.
// Works for PostgreSQL, MySQL, and ClickHouse.
func (a *App) ListServerDatabases(cfg driver.ConnectionConfig) ([]string, error) {
	tmpCfg := cfg

	var query string
	switch cfg.Type {
	case driver.Postgres:
		if tmpCfg.Database == "" {
			tmpCfg.Database = "postgres"
		}
		query = "SELECT datname FROM pg_database WHERE datistemplate = false ORDER BY datname"
	case driver.MySQL:
		if tmpCfg.Database == "" {
			tmpCfg.Database = "information_schema"
		}
		query = "SELECT SCHEMA_NAME FROM information_schema.SCHEMATA WHERE SCHEMA_NAME NOT IN ('information_schema','performance_schema','mysql','sys') ORDER BY SCHEMA_NAME"
	case driver.ClickHouse:
		if tmpCfg.Database == "" {
			tmpCfg.Database = "default"
		}
		query = "SELECT name FROM system.databases WHERE name NOT IN ('system','INFORMATION_SCHEMA','information_schema') ORDER BY name"
	default:
		return nil, fmt.Errorf("database discovery not supported for %s", cfg.Type)
	}

	d, err := driver.New(tmpCfg.Type)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
	defer cancel()

	if err := d.Connect(ctx, tmpCfg); err != nil {
		return nil, fmt.Errorf("connection failed: %w", err)
	}
	defer d.Disconnect()

	result, err := d.Execute(ctx, query, 200)
	if err != nil {
		return nil, fmt.Errorf("list databases: %w", err)
	}

	var dbs []string
	for _, row := range result.Rows {
		if len(row) > 0 && row[0] != nil {
			dbs = append(dbs, fmt.Sprintf("%v", row[0]))
		}
	}
	return dbs, nil
}

// =========================================================================
// Saved Connections
// =========================================================================

// ListSavedConnections returns all stored connection configs.
func (a *App) ListSavedConnections() ([]store.SavedConnection, error) {
	if a.store == nil {
		return nil, fmt.Errorf("store not initialized")
	}
	return a.store.ListConnections()
}

// SaveConnection persists a connection config.
func (a *App) SaveConnection(cfg driver.ConnectionConfig) error {
	if a.store == nil {
		return fmt.Errorf("store not initialized")
	}
	if cfg.ID == "" {
		cfg.ID = uuid.New().String()
	}
	cfgJSON, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	return a.store.SaveConnection(cfg.ID, cfg.Name, string(cfg.Type), string(cfgJSON))
}

// DeleteConnection removes a stored connection.
func (a *App) DeleteConnection(id string) error {
	if a.store == nil {
		return fmt.Errorf("store not initialized")
	}
	return a.store.DeleteConnection(id)
}

// =========================================================================
// Schema Browser
// =========================================================================

// ListDatabases returns datasets (BQ), databases (PG/MY/Mongo), or "main" (SQLite).
func (a *App) ListDatabases() ([]string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.driver == nil {
		return nil, fmt.Errorf("not connected")
	}
	return a.driver.ListDatabases(a.ctx)
}

// ListTables returns tables/views/collections in a database.
func (a *App) ListTables(database string) ([]driver.TableInfo, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.driver == nil {
		return nil, fmt.Errorf("not connected")
	}
	return a.driver.ListTables(a.ctx, database)
}

// GetTableSchema returns column details for a specific table.
func (a *App) GetTableSchema(database, table string) (*driver.TableInfo, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.driver == nil {
		return nil, fmt.Errorf("not connected")
	}
	return a.driver.GetTableSchema(a.ctx, database, table)
}

// PreviewTable returns the first N rows of a table.
func (a *App) PreviewTable(database, table string, limit int) (*driver.QueryResult, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.driver == nil {
		return nil, fmt.Errorf("not connected")
	}
	return a.driver.PreviewTable(a.ctx, database, table, limit)
}

// =========================================================================
// Query Execution
// =========================================================================

// DryRun validates a query and estimates cost (for BQ/ClickHouse).
func (a *App) DryRun(sql string) (*driver.DryRunResult, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.driver == nil {
		return nil, fmt.Errorf("not connected")
	}
	return a.driver.DryRun(a.ctx, sql)
}

// ExplainQuery returns a structured query execution plan.
func (a *App) ExplainQuery(sql string) (*driver.ExplainResult, error) {
	a.mu.RLock()
	d := a.driver
	a.mu.RUnlock()

	if d == nil {
		return nil, fmt.Errorf("not connected")
	}

	logger.Info("explain: sql=%.100s", sql)

	// Use EXPLAIN with JSON format where supported
	var explainSQL string
	switch d.Type() {
	case driver.Postgres:
		explainSQL = "EXPLAIN (FORMAT JSON) " + sql
	case driver.MySQL:
		explainSQL = "EXPLAIN FORMAT=JSON " + sql
	case driver.SQLiteType:
		explainSQL = "EXPLAIN QUERY PLAN " + sql
	default:
		// Fallback: use regular EXPLAIN
		explainSQL = "EXPLAIN " + sql
	}

	result, err := d.Execute(a.ctx, explainSQL, 1000)
	if err != nil {
		return nil, fmt.Errorf("explain failed: %w", err)
	}

	// Collect raw text
	var rawLines []string
	for _, row := range result.Rows {
		for _, cell := range row {
			if cell != nil {
				rawLines = append(rawLines, fmt.Sprintf("%v", cell))
			}
		}
	}
	rawText := strings.Join(rawLines, "\n")

	plan := parseExplainPlan(d.Type(), rawText, result)
	return &driver.ExplainResult{
		Plan:    plan,
		RawText: rawText,
	}, nil
}

// parseExplainPlan converts raw EXPLAIN output into a structured tree.
func parseExplainPlan(dt driver.DriverType, rawText string, result *driver.QueryResult) driver.ExplainNode {
	switch dt {
	case driver.Postgres:
		return parsePostgresExplainJSON(rawText)
	case driver.MySQL:
		return parseMySQLExplainJSON(rawText)
	case driver.SQLiteType:
		return parseSQLiteExplainPlan(result)
	default:
		return driver.ExplainNode{Operation: "Query Plan", Details: rawText}
	}
}

func parsePostgresExplainJSON(raw string) driver.ExplainNode {
	var plans []struct {
		Plan json.RawMessage `json:"Plan"`
	}
	if err := json.Unmarshal([]byte(raw), &plans); err != nil || len(plans) == 0 {
		return driver.ExplainNode{Operation: "Query Plan", Details: raw}
	}
	return parsePGNode(plans[0].Plan)
}

func parsePGNode(data json.RawMessage) driver.ExplainNode {
	var node struct {
		NodeType      string            `json:"Node Type"`
		RelationName  string            `json:"Relation Name"`
		PlanRows      int64             `json:"Plan Rows"`
		TotalCost     float64           `json:"Total Cost"`
		Plans         []json.RawMessage `json:"Plans"`
		JoinType      string            `json:"Join Type"`
		IndexName     string            `json:"Index Name"`
		Filter        string            `json:"Filter"`
		SortKey       []string          `json:"Sort Key"`
		HashCond      string            `json:"Hash Cond"`
		IndexCond     string            `json:"Index Cond"`
	}
	if err := json.Unmarshal(data, &node); err != nil {
		return driver.ExplainNode{Operation: "Unknown"}
	}

	details := ""
	if node.JoinType != "" {
		details = node.JoinType
	}
	if node.IndexName != "" {
		details += " idx=" + node.IndexName
	}
	if node.Filter != "" {
		details += " filter=" + node.Filter
	}
	if node.HashCond != "" {
		details += " on=" + node.HashCond
	}
	if node.IndexCond != "" {
		details += " cond=" + node.IndexCond
	}
	if len(node.SortKey) > 0 {
		details += " by=" + strings.Join(node.SortKey, ", ")
	}

	en := driver.ExplainNode{
		Operation:     node.NodeType,
		Table:         node.RelationName,
		EstimatedRows: node.PlanRows,
		Cost:          node.TotalCost,
		Details:       strings.TrimSpace(details),
	}
	for _, child := range node.Plans {
		en.Children = append(en.Children, parsePGNode(child))
	}
	return en
}

func parseMySQLExplainJSON(raw string) driver.ExplainNode {
	var wrapper struct {
		QueryBlock struct {
			SelectID   int    `json:"select_id"`
			Table      *struct {
				TableName    string `json:"table_name"`
				AccessType   string `json:"access_type"`
				RowsExamined int64  `json:"rows_examined_per_scan"`
				Key          string `json:"key"`
				UsedKeyParts []string `json:"used_key_parts"`
				AttachedCondition string `json:"attached_condition"`
			} `json:"table"`
			OrderingOp *struct {
				UsingFilesort bool `json:"using_filesort"`
			} `json:"ordering_operation"`
		} `json:"query_block"`
	}
	if err := json.Unmarshal([]byte(raw), &wrapper); err != nil {
		return driver.ExplainNode{Operation: "Query Plan", Details: raw}
	}

	root := driver.ExplainNode{Operation: "Query Block"}
	if t := wrapper.QueryBlock.Table; t != nil {
		details := t.AccessType
		if t.Key != "" {
			details += " key=" + t.Key
		}
		if t.AttachedCondition != "" {
			details += " where=" + t.AttachedCondition
		}
		root.Children = append(root.Children, driver.ExplainNode{
			Operation:     "Table Scan",
			Table:         t.TableName,
			EstimatedRows: t.RowsExamined,
			Details:       details,
		})
	}
	if wrapper.QueryBlock.OrderingOp != nil && wrapper.QueryBlock.OrderingOp.UsingFilesort {
		root.Children = append(root.Children, driver.ExplainNode{
			Operation: "Filesort",
			Details:   "using temporary sort",
		})
	}
	return root
}

func parseSQLiteExplainPlan(result *driver.QueryResult) driver.ExplainNode {
	root := driver.ExplainNode{Operation: "Query Plan"}
	// EXPLAIN QUERY PLAN returns: id, parent, notused, detail
	nodes := map[int]*driver.ExplainNode{}
	nodes[0] = &root

	for _, row := range result.Rows {
		if len(row) < 4 {
			continue
		}
		id, _ := toInt(row[0])
		parent, _ := toInt(row[1])
		detail := fmt.Sprintf("%v", row[3])

		node := driver.ExplainNode{Operation: detail}
		if p, ok := nodes[parent]; ok {
			p.Children = append(p.Children, node)
			nodes[id] = &p.Children[len(p.Children)-1]
		} else {
			root.Children = append(root.Children, node)
			nodes[id] = &root.Children[len(root.Children)-1]
		}
	}
	return root
}

func toInt(v interface{}) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int64:
		return int(n), true
	case int:
		return n, true
	}
	return 0, false
}

// Execute runs a query and saves it to history.
// Results are cached for 5 minutes to avoid re-querying identical data.
func (a *App) Execute(sql string, limit int) (*driver.QueryResult, error) {
	a.mu.RLock()
	d := a.driver
	connID := a.connID
	connName := a.connName
	driverType := a.connType
	a.mu.RUnlock()

	if d == nil {
		return nil, fmt.Errorf("not connected")
	}
	if limit <= 0 {
		limit = 10000
	}

	logger.Info("execute: limit=%d sql=%.100s", limit, sql)

	// Check cache first
	cacheKey := a.queryCacheKey(connID, sql, limit)
	if cached := a.getFromCache(cacheKey); cached != nil {
		// Return cached result with a flag indicating it came from cache
		cachedResult := *cached
		localHit := true
		cachedResult.CacheHit = &localHit
		// Still save to history as a cache hit
		if a.store != nil {
			entry := store.HistoryEntry{
				ID:             uuid.New().String(),
				ConnectionID:   connID,
				ConnectionName: connName,
				DriverType:     string(driverType),
				SQL:            sql,
				Status:         "completed",
				CacheHit:       true,
			}
			rc := cachedResult.RowCount
			entry.RowCount = &rc
			dur := cachedResult.DurationMs
			entry.DurationMs = &dur
			entry.BytesProcessed = cachedResult.BytesProcessed
			entry.CostUSD = cachedResult.CostUSD
			a.store.AddHistory(entry)
		}
		return &cachedResult, nil
	}

	// Query timeout: 5 minutes max. Prevents hanging queries.
	queryCtx, queryCancel := context.WithTimeout(a.ctx, 5*time.Minute)
	defer queryCancel()

	result, err := d.Execute(queryCtx, sql, limit)

	// Save to history regardless of success/failure
	if a.store != nil {
		entry := store.HistoryEntry{
			ID:             uuid.New().String(),
			ConnectionID:   connID,
			ConnectionName: connName,
			DriverType:     string(driverType),
			SQL:            sql,
		}
		if err != nil {
			entry.Status = "failed"
			entry.Error = err.Error()
		} else {
			entry.Status = "completed"
			entry.RowCount = &result.RowCount
			entry.DurationMs = &result.DurationMs
			entry.BytesProcessed = result.BytesProcessed
			entry.CostUSD = result.CostUSD
			if result.CacheHit != nil {
				entry.CacheHit = *result.CacheHit
			}
		}
		a.store.AddHistory(entry)
	}

	// Cache successful results
	if err == nil && result != nil {
		a.putInCache(cacheKey, connID, sql, result)
	}

	return result, err
}

// ClearQueryCache removes all cached query results.
func (a *App) ClearQueryCache() {
	a.cacheMu.Lock()
	defer a.cacheMu.Unlock()
	a.queryCache = make(map[string]*queryCacheEntry)
}

func (a *App) queryCacheKey(connID, sql string, limit int) string {
	raw := fmt.Sprintf("%s|%s|%d", connID, sql, limit)
	h := fmt.Sprintf("%x", sha256.Sum256([]byte(raw)))
	return h
}

func (a *App) getFromCache(key string) *driver.QueryResult {
	a.cacheMu.RLock()
	defer a.cacheMu.RUnlock()

	if a.queryCache == nil {
		return nil
	}
	entry, ok := a.queryCache[key]
	if !ok {
		return nil
	}
	if time.Since(entry.cachedAt) > queryCacheTTL {
		return nil // expired
	}
	return entry.result
}

func (a *App) putInCache(key, connID, sql string, result *driver.QueryResult) {
	a.cacheMu.Lock()
	defer a.cacheMu.Unlock()

	if a.queryCache == nil {
		a.queryCache = make(map[string]*queryCacheEntry)
	}

	// Evict expired entries and enforce max size
	if len(a.queryCache) >= queryCacheMaxSize {
		oldest := ""
		oldestTime := time.Now()
		for k, v := range a.queryCache {
			if time.Since(v.cachedAt) > queryCacheTTL {
				delete(a.queryCache, k)
			} else if v.cachedAt.Before(oldestTime) {
				oldest = k
				oldestTime = v.cachedAt
			}
		}
		// If still at max, evict oldest
		if len(a.queryCache) >= queryCacheMaxSize && oldest != "" {
			delete(a.queryCache, oldest)
		}
	}

	a.queryCache[key] = &queryCacheEntry{
		result:   result,
		cachedAt: time.Now(),
		sql:      sql,
		connID:   connID,
	}
}

// CancelQuery cancels the currently running query.
func (a *App) CancelQuery() error {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.driver == nil {
		return fmt.Errorf("not connected")
	}
	return a.driver.Cancel()
}

// =========================================================================
// Query History
// =========================================================================

// GetHistory returns paginated query history.
func (a *App) GetHistory(search string, limit, offset int) ([]store.HistoryEntry, error) {
	if a.store == nil {
		return nil, fmt.Errorf("store not initialized")
	}
	return a.store.ListHistory(search, limit, offset)
}

// DeleteHistory removes a single history entry.
func (a *App) DeleteHistory(id string) error {
	if a.store == nil {
		return fmt.Errorf("store not initialized")
	}
	return a.store.DeleteHistory(id)
}

// ClearHistory removes all history entries.
func (a *App) ClearHistory() error {
	if a.store == nil {
		return fmt.Errorf("store not initialized")
	}
	return a.store.ClearHistory()
}

// GetHistoryStats returns aggregate stats about query history.
func (a *App) GetHistoryStats() (*store.HistoryStats, error) {
	if a.store == nil {
		return nil, fmt.Errorf("store not initialized")
	}
	return a.store.GetHistoryStats()
}

// =========================================================================
// Saved Queries
// =========================================================================

// ListSavedQueries returns all saved queries, optionally filtered by search.
func (a *App) ListSavedQueries(search string) ([]store.SavedQuery, error) {
	if a.store == nil {
		return nil, fmt.Errorf("store not initialized")
	}
	return a.store.ListSavedQueries(search)
}

// SaveQuery persists a query with name, description, and tags.
func (a *App) SaveQuery(name, sql, description, tagsJSON string) (*store.SavedQuery, error) {
	if a.store == nil {
		return nil, fmt.Errorf("store not initialized")
	}
	q := store.SavedQuery{
		ID:           uuid.New().String(),
		Name:         name,
		Description:  description,
		SQL:          sql,
		Tags:         tagsJSON,
		ConnectionID: a.connID,
	}
	if err := a.store.SaveQuery(q); err != nil {
		return nil, err
	}
	return &q, nil
}

// DeleteSavedQuery removes a saved query.
func (a *App) DeleteSavedQuery(id string) error {
	if a.store == nil {
		return fmt.Errorf("store not initialized")
	}
	return a.store.DeleteSavedQuery(id)
}

// GetQueryBySlug loads a shared query by its URL slug.
func (a *App) GetQueryBySlug(slug string) (*store.SavedQuery, error) {
	if a.store == nil {
		return nil, fmt.Errorf("store not initialized")
	}
	return a.store.GetQueryBySlug(slug)
}

// =========================================================================
// Query Templates (Beginner Guidance)
// =========================================================================

// ListTemplates returns built-in query templates, filtered by driver type.
func (a *App) ListTemplates(driverType string) ([]store.QueryTemplate, error) {
	if a.store == nil {
		return nil, fmt.Errorf("store not initialized")
	}
	return a.store.ListTemplates(driverType)
}

// =========================================================================
// AI Assistant
// =========================================================================

// AiChat sends a message to the AI and streams the response via Wails events.
// Frontend listens for "ai:chunk" events to display streaming text.
// The conversation history is loaded from the store and included in context.
// editorContext is a JSON string describing the current editor state (SQL, results, errors).
func (a *App) AiChat(message, conversationID, editorContext string) error {
	if a.aiProv == nil || !a.aiProv.IsConfigured() {
		logger.Warn("ai chat: provider not configured")
		return fmt.Errorf("AI provider not configured. Go to Settings to set up an API key.")
	}
	logger.Info("ai chat: conv=%s msg=%.80s", conversationID, message)

	// Ensure conversation exists in store
	if a.store != nil {
		convs, _ := a.store.ListConversations()
		found := false
		for _, c := range convs {
			if c.ID == conversationID {
				found = true
				break
			}
		}
		if !found {
			a.store.CreateConversation(conversationID, "New Chat")
		}
	}

	// Save user message
	if a.store != nil {
		a.store.AddChatMessage(uuid.NewString(), conversationID, "user", message)
	}

	// Build schema context from current connection
	var schema *ai.SchemaContext
	if a.driver != nil {
		schema = a.buildSchemaContext()
	}

	// Load conversation history for context
	var messages []ai.Message
	if a.store != nil {
		stored, err := a.store.ListChatMessages(conversationID)
		if err == nil {
			// Include recent history (cap at last 20 messages to stay within context)
			start := 0
			if len(stored) > 20 {
				start = len(stored) - 20
			}
			for _, m := range stored[start:] {
				messages = append(messages, ai.Message{Role: m.Role, Content: m.Content})
			}
		}
	}

	// If no history loaded, at least include current message
	if len(messages) == 0 {
		messages = []ai.Message{{Role: "user", Content: message}}
	}

	// Inject editor context as a system message so the AI can "see" the current screen
	if editorContext != "" {
		contextMsg := ai.Message{
			Role:    "system",
			Content: "CURRENT EDITOR STATE (what the user sees on screen right now):\n" + editorContext,
		}
		// Insert before the last user message so the AI has context for the question
		if len(messages) > 1 {
			messages = append(messages[:len(messages)-1], contextMsg, messages[len(messages)-1])
		} else {
			messages = append([]ai.Message{contextMsg}, messages...)
		}
	}

	model, _ := a.store.GetSetting("ai_model")

	// Stream the response, collecting chunks for persistence
	var fullResponse strings.Builder
	err := a.aiProv.StreamChat(a.ctx, messages, schema, model, func(chunk string) {
		fullResponse.WriteString(chunk)
		wailsRuntime.EventsEmit(a.ctx, "ai:chunk", map[string]string{
			"text":            chunk,
			"conversation_id": conversationID,
		})
	})

	// Handle context overflow: trim history and retry once
	if err != nil && isContextOverflow(err) && len(messages) > 4 {
		// Keep system prompt + last 6 messages (3 exchanges)
		trimmed := []ai.Message{messages[0]}
		if len(messages) > 7 {
			trimmed = append(trimmed, messages[len(messages)-6:]...)
		} else {
			trimmed = append(trimmed, messages[2:]...)
		}
		fullResponse.Reset()
		wailsRuntime.EventsEmit(a.ctx, "ai:chunk", map[string]string{
			"text":            "\n\n_(Conversation too long — trimmed to recent messages)_\n\n",
			"conversation_id": conversationID,
		})
		err = a.aiProv.StreamChat(a.ctx, trimmed, schema, model, func(chunk string) {
			fullResponse.WriteString(chunk)
			wailsRuntime.EventsEmit(a.ctx, "ai:chunk", map[string]string{
				"text":            chunk,
				"conversation_id": conversationID,
			})
		})
	}

	// Save assistant response
	if a.store != nil && fullResponse.Len() > 0 {
		a.store.AddChatMessage(uuid.NewString(), conversationID, "assistant", fullResponse.String())
	}

	// Auto-generate title for new conversations (after first exchange)
	if a.store != nil {
		convs, _ := a.store.ListConversations()
		for _, c := range convs {
			if c.ID == conversationID && c.Title == "New Chat" {
				go a.generateConversationTitle(conversationID, message)
				break
			}
		}
	}

	// Make error messages user-friendly
	if err != nil {
		if isContextOverflow(err) {
			return fmt.Errorf("conversation is too long. Start a new chat to continue.")
		}
		if isRateLimited(err) {
			return fmt.Errorf("rate limited by AI provider. Please wait a moment and try again.")
		}
	}

	return err
}

// isContextOverflow detects token/context limit errors from AI providers.
func isContextOverflow(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "context_length") ||
		strings.Contains(msg, "context window") ||
		strings.Contains(msg, "maximum context") ||
		strings.Contains(msg, "too many tokens") ||
		strings.Contains(msg, "max_tokens") ||
		strings.Contains(msg, "token limit") ||
		strings.Contains(msg, "request too large")
}

// isRateLimited detects rate limit errors from AI providers.
func isRateLimited(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "rate_limit") ||
		strings.Contains(msg, "rate limit") ||
		strings.Contains(msg, "429") ||
		strings.Contains(msg, "too many requests")
}

// generateConversationTitle uses AI to create a short title from the first message.
func (a *App) generateConversationTitle(conversationID, firstMessage string) {
	if a.aiProv == nil || !a.aiProv.IsConfigured() {
		return
	}

	prompt := fmt.Sprintf(`Generate a very short title (3-6 words, no quotes) for a chat conversation that started with this message:
"%s"

Return ONLY the title text, nothing else.`, firstMessage)

	model, _ := a.store.GetSetting("ai_model")
	resp, err := a.aiProv.Complete(a.ctx, []ai.Message{
		{Role: "user", Content: prompt},
	}, nil, model)
	if err != nil {
		return
	}

	title := strings.TrimSpace(resp)
	// Clean up: remove quotes, limit length
	title = strings.Trim(title, `"'`)
	if len(title) > 50 {
		title = title[:50]
	}
	if title == "" {
		return
	}

	a.store.UpdateConversationTitle(conversationID, title)

	// Notify frontend of title update
	wailsRuntime.EventsEmit(a.ctx, "ai:title-update", map[string]string{
		"conversation_id": conversationID,
		"title":           title,
	})
}

// ListAiConversations returns all saved AI conversations.
func (a *App) ListAiConversations() ([]store.Conversation, error) {
	if a.store == nil {
		return nil, fmt.Errorf("store not initialized")
	}
	return a.store.ListConversations()
}

// GetAiMessages returns messages for a conversation.
func (a *App) GetAiMessages(conversationID string) ([]store.ChatMessage, error) {
	if a.store == nil {
		return nil, fmt.Errorf("store not initialized")
	}
	return a.store.ListChatMessages(conversationID)
}

// DeleteAiConversation deletes a conversation and all its messages.
func (a *App) DeleteAiConversation(id string) error {
	if a.store == nil {
		return fmt.Errorf("store not initialized")
	}
	return a.store.DeleteConversation(id)
}

// RenameAiConversation updates a conversation title.
func (a *App) RenameAiConversation(id, title string) error {
	if a.store == nil {
		return fmt.Errorf("store not initialized")
	}
	return a.store.UpdateConversationTitle(id, title)
}

// GetAiProviders returns available AI providers and their config status.
func (a *App) GetAiProviders() []ai.ProviderInfo {
	var providers []ai.ProviderInfo
	for _, name := range ai.AvailableProviders() {
		prov, err := ai.New(name, "", "")
		if err != nil {
			continue
		}
		providers = append(providers, ai.ProviderInfo{
			Name:         prov.Name(),
			DefaultModel: prov.DefaultModel(),
			MinModel:     prov.MinModel(),
			Configured:   false, // Will check with actual key
		})
	}

	// Mark the active provider as configured
	if a.aiProv != nil {
		for i, p := range providers {
			if p.Name == a.aiProv.Name() {
				providers[i].Configured = a.aiProv.IsConfigured()
			}
		}
	}
	return providers
}

// AiSettings holds the current AI configuration for the frontend.
type AiSettings struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	APIKey   string `json:"api_key"`
	Endpoint string `json:"endpoint"`
}

// GetAiSettings returns the current AI configuration.
func (a *App) GetAiSettings() *AiSettings {
	s := &AiSettings{}
	if a.store != nil {
		s.Provider, _ = a.store.GetSetting("ai_provider")
		s.Model, _ = a.store.GetSetting("ai_model")
		s.APIKey, _ = a.store.GetSecretSetting("ai_api_key")
		s.Endpoint, _ = a.store.GetSetting("ai_endpoint")
	}
	return s
}

// SetAiSettings validates the key with a test call, then saves.
func (a *App) SetAiSettings(providerName, model, apiKey, endpoint string) error {
	logger.Info("set ai settings: provider=%s model=%s endpoint=%s", providerName, model, endpoint)

	prov, err := ai.New(providerName, apiKey, endpoint)
	if err != nil {
		logger.Error("ai provider init: %v", err)
		return err
	}

	// Validate the key with a lightweight test call
	testMsgs := []ai.Message{{Role: "user", Content: "Hi"}}
	useModel := model
	if useModel == "" {
		useModel = prov.DefaultModel()
	}
	_, err = prov.Complete(a.ctx, testMsgs, &ai.SchemaContext{}, useModel)
	if err != nil {
		logger.Error("ai validation failed: %v", err)
		return fmt.Errorf("API key validation failed: %w", err)
	}

	a.aiProv = prov
	logger.Info("ai provider configured: %s/%s", providerName, useModel)

	// Persist settings
	if a.store != nil {
		a.store.SetSetting("ai_provider", providerName)
		a.store.SetSetting("ai_model", model)
		a.store.SetSecretSetting("ai_api_key", apiKey)
		a.store.SetSetting("ai_endpoint", endpoint)
	}
	return nil
}

// =========================================================================
// AI Query Optimizer
// =========================================================================

// OptimizeResult is returned by OptimizeQuery.
type OptimizeResult struct {
	OriginalSQL     string               `json:"original_sql"`
	OptimizedSQL    string               `json:"optimized_sql"`
	Explanation     string               `json:"explanation"`
	Iterations      int                  `json:"iterations"`
	OriginalDryRun  *driver.DryRunResult `json:"original_dry_run,omitempty"`
	OptimizedDryRun *driver.DryRunResult `json:"optimized_dry_run,omitempty"`
	Improvements    []string             `json:"improvements"`
}

// OptimizeQuery uses AI to iteratively improve a SQL query, using dry-run
// feedback to verify each optimization actually reduces cost/scan.
// It runs up to 3 optimization rounds, keeping the best version.
func (a *App) OptimizeQuery(sql string) (*OptimizeResult, error) {
	if a.aiProv == nil || !a.aiProv.IsConfigured() {
		return nil, fmt.Errorf("AI provider not configured")
	}
	if a.driver == nil {
		return nil, fmt.Errorf("not connected to a database")
	}

	// Step 1: Dry-run the original query to get baseline cost
	origDryRun, _ := a.driver.DryRun(a.ctx, sql)

	// Step 2: Build schema context
	schema := a.buildSchemaContext()

	a.mu.RLock()
	dType := a.connType
	a.mu.RUnlock()

	// Step 3: Iterative optimization (max 3 rounds)
	bestSQL := sql
	bestDryRun := origDryRun
	var allImprovements []string
	iterations := 0

	for round := 0; round < 3; round++ {
		iterations++

		// Build optimization prompt with dry-run feedback
		prompt := buildOptimizePrompt(bestSQL, bestDryRun, dType, round)

		model, _ := a.store.GetSetting("ai_model")
		resp, err := a.aiProv.Complete(a.ctx, []ai.Message{
			{Role: "system", Content: ai.BuildSystemPrompt(schema)},
			{Role: "user", Content: prompt},
		}, schema, model)
		if err != nil {
			break // AI failed, return what we have
		}

		// Extract SQL and explanation from response
		optimizedSQL, explanation, improvements := parseOptimizeResponse(resp)
		_ = explanation
		if optimizedSQL == "" || optimizedSQL == bestSQL {
			break // No further optimizations possible
		}

		// Dry-run the optimized version
		newDryRun, err := a.driver.DryRun(a.ctx, optimizedSQL)
		if err != nil || (newDryRun != nil && !newDryRun.Valid) {
			// Optimized query is invalid, stop here
			break
		}

		// Check if the optimization actually improved things
		if isBetterDryRun(newDryRun, bestDryRun) {
			bestSQL = optimizedSQL
			bestDryRun = newDryRun
			allImprovements = append(allImprovements, improvements...)
		} else {
			// Optimization didn't help, stop iterating
			break
		}
	}

	// Build final explanation
	finalExplanation := ""
	if bestSQL != sql {
		finalExplanation = "Query optimized"
		if len(allImprovements) > 0 {
			finalExplanation = strings.Join(allImprovements, "; ")
		}
	} else {
		finalExplanation = "No further optimizations found — this query is already efficient."
	}

	return &OptimizeResult{
		OriginalSQL:     sql,
		OptimizedSQL:    bestSQL,
		Explanation:     finalExplanation,
		Iterations:      iterations,
		OriginalDryRun:  origDryRun,
		OptimizedDryRun: bestDryRun,
		Improvements:    allImprovements,
	}, nil
}

func buildOptimizePrompt(sql string, dryRun *driver.DryRunResult, dType driver.DriverType, round int) string {
	var sb strings.Builder

	sb.WriteString("Optimize this SQL query for better performance and lower cost. ")

	if round == 0 {
		sb.WriteString("Focus on the most impactful optimizations first.\n\n")
	} else {
		sb.WriteString("This is a follow-up optimization pass. Look for additional improvements.\n\n")
	}

	sb.WriteString("Current query:\n```sql\n")
	sb.WriteString(sql)
	sb.WriteString("\n```\n\n")

	if dryRun != nil && dryRun.Valid {
		sb.WriteString("Current dry-run metrics:\n")
		if dryRun.EstimatedBytes > 0 {
			gb := float64(dryRun.EstimatedBytes) / (1024 * 1024 * 1024)
			sb.WriteString(fmt.Sprintf("- Estimated scan: %.2f GB\n", gb))
		}
		if dryRun.EstimatedCost > 0 {
			sb.WriteString(fmt.Sprintf("- Estimated cost: $%.4f\n", dryRun.EstimatedCost))
		}
		if dryRun.EstimatedRows > 0 {
			sb.WriteString(fmt.Sprintf("- Estimated rows: %d\n", dryRun.EstimatedRows))
		}
		if len(dryRun.ReferencedTables) > 0 {
			sb.WriteString(fmt.Sprintf("- Referenced tables: %s\n", strings.Join(dryRun.ReferencedTables, ", ")))
		}
	}

	sb.WriteString("\nOptimization strategies to consider:\n")

	if dType == driver.BigQuery {
		sb.WriteString(`- Replace SELECT * with specific columns (BQ charges per column scanned)
- Add partition filters (WHERE _PARTITIONTIME, date columns) to reduce scan
- Use approximate functions (APPROX_COUNT_DISTINCT) when exact counts aren't needed
- Avoid self-joins on large tables; use window functions instead
- Push filters closer to the data source (inside subqueries/CTEs)
- Consider LIMIT if full result set isn't needed
`)
	} else {
		sb.WriteString(`- Replace SELECT * with specific columns
- Add or improve WHERE clauses to leverage indexes
- Avoid functions on indexed columns in WHERE (prevents index use)
- Use EXISTS instead of IN for subqueries
- Add LIMIT if full result set isn't needed
- Consider using CTEs for readability and potential optimization
`)
	}

	sb.WriteString(`
CRITICAL RULES:
- The optimized query MUST return the same logical result as the original
- Do NOT change the query semantics or filter out rows the original would return
- Do NOT add columns or remove columns unless replacing SELECT *
- Only use table and column names that exist in the schema
- Do NOT invent or hallucinate table names, column names, or functions that don't exist

Return your response in EXACTLY this format:
OPTIMIZED_SQL:
` + "```sql" + `
<your optimized query here>
` + "```" + `

IMPROVEMENTS:
- <improvement 1>
- <improvement 2>

EXPLANATION:
<brief explanation of why these changes help>
`)

	return sb.String()
}

func parseOptimizeResponse(resp string) (sql, explanation string, improvements []string) {
	// Extract SQL between ```sql and ```
	sqlStart := strings.Index(resp, "```sql")
	if sqlStart == -1 {
		sqlStart = strings.Index(resp, "```SQL")
	}
	if sqlStart == -1 {
		return "", "", nil
	}
	sqlStart = strings.Index(resp[sqlStart:], "\n") + sqlStart + 1
	sqlEnd := strings.Index(resp[sqlStart:], "```")
	if sqlEnd == -1 {
		return "", "", nil
	}
	sql = strings.TrimSpace(resp[sqlStart : sqlStart+sqlEnd])

	// Extract improvements
	impStart := strings.Index(resp, "IMPROVEMENTS:")
	expStart := strings.Index(resp, "EXPLANATION:")

	if impStart != -1 {
		end := len(resp)
		if expStart > impStart {
			end = expStart
		}
		impSection := resp[impStart+len("IMPROVEMENTS:") : end]
		for _, line := range strings.Split(impSection, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") {
				improvements = append(improvements, strings.TrimPrefix(strings.TrimPrefix(line, "- "), "* "))
			}
		}
	}

	if expStart != -1 {
		explanation = strings.TrimSpace(resp[expStart+len("EXPLANATION:"):])
		if idx := strings.Index(explanation, "\n\n"); idx != -1 {
			explanation = explanation[:idx]
		}
	}

	return sql, explanation, improvements
}

func isBetterDryRun(newDR, oldDR *driver.DryRunResult) bool {
	if newDR == nil || oldDR == nil {
		return newDR != nil
	}
	if newDR.EstimatedBytes > 0 && oldDR.EstimatedBytes > 0 {
		return newDR.EstimatedBytes < oldDR.EstimatedBytes
	}
	if newDR.EstimatedRows > 0 && oldDR.EstimatedRows > 0 {
		return newDR.EstimatedRows < oldDR.EstimatedRows
	}
	return true
}

// =========================================================================
// Export
// =========================================================================

// ExportCSV runs a query and saves results as CSV. Returns the file path.
func (a *App) ExportCSV(sql string) (string, error) {
	result, err := a.Execute(sql, 100000)
	if err != nil {
		return "", err
	}

	path, err := wailsRuntime.SaveFileDialog(a.ctx, wailsRuntime.SaveDialogOptions{
		Title:           "Export as CSV",
		DefaultFilename: "export.csv",
		Filters: []wailsRuntime.FileFilter{
			{DisplayName: "CSV Files", Pattern: "*.csv"},
		},
	})
	if err != nil || path == "" {
		return "", fmt.Errorf("export cancelled")
	}

	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	// Header
	for i, col := range result.Columns {
		if i > 0 {
			f.WriteString(",")
		}
		f.WriteString(csvEscape(col))
	}
	f.WriteString("\n")

	// Rows
	for _, row := range result.Rows {
		for i, val := range row {
			if i > 0 {
				f.WriteString(",")
			}
			f.WriteString(csvEscape(fmt.Sprintf("%v", val)))
		}
		f.WriteString("\n")
	}
	return path, nil
}

// ExportJSON runs a query and saves results as JSON. Returns the file path.
func (a *App) ExportJSON(sql string) (string, error) {
	result, err := a.Execute(sql, 100000)
	if err != nil {
		return "", err
	}

	path, err := wailsRuntime.SaveFileDialog(a.ctx, wailsRuntime.SaveDialogOptions{
		Title:           "Export as JSON",
		DefaultFilename: "export.json",
		Filters: []wailsRuntime.FileFilter{
			{DisplayName: "JSON Files", Pattern: "*.json"},
		},
	})
	if err != nil || path == "" {
		return "", fmt.Errorf("export cancelled")
	}

	// Convert to array of objects
	var records []map[string]interface{}
	for _, row := range result.Rows {
		record := make(map[string]interface{})
		for i, col := range result.Columns {
			if i < len(row) {
				record[col] = row[i]
			}
		}
		records = append(records, record)
	}

	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return "", err
	}
	return path, os.WriteFile(path, data, 0644)
}

// ExportExcel runs a query and saves results as an Excel (.xlsx) file. Returns the file path.
func (a *App) ExportExcel(sql string) (string, error) {
	result, err := a.Execute(sql, 100000)
	if err != nil {
		return "", err
	}

	path, err := wailsRuntime.SaveFileDialog(a.ctx, wailsRuntime.SaveDialogOptions{
		Title:           "Export as Excel",
		DefaultFilename: "export.xlsx",
		Filters: []wailsRuntime.FileFilter{
			{DisplayName: "Excel Files", Pattern: "*.xlsx"},
		},
	})
	if err != nil || path == "" {
		return "", fmt.Errorf("export cancelled")
	}

	f := excelize.NewFile()
	sheet := "Sheet1"

	// Header row with bold style
	headerStyle, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true},
		Fill: excelize.Fill{Type: "pattern", Color: []string{"#E8E8E8"}, Pattern: 1},
	})
	for i, col := range result.Columns {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		f.SetCellValue(sheet, cell, col)
		f.SetCellStyle(sheet, cell, cell, headerStyle)
	}

	// Data rows
	for rowIdx, row := range result.Rows {
		for colIdx, val := range row {
			cell, _ := excelize.CoordinatesToCellName(colIdx+1, rowIdx+2)
			if val == nil {
				f.SetCellValue(sheet, cell, "")
			} else {
				f.SetCellValue(sheet, cell, val)
			}
		}
	}

	// Auto-fit column widths (approximate)
	for i, col := range result.Columns {
		width := float64(len(col)) + 4
		if width < 12 {
			width = 12
		}
		if width > 50 {
			width = 50
		}
		colName, _ := excelize.ColumnNumberToName(i + 1)
		f.SetColWidth(sheet, colName, colName, width)
	}

	if err := f.SaveAs(path); err != nil {
		return "", err
	}
	return path, nil
}

// =========================================================================
// Discovery
// =========================================================================

// DatabaseInfo describes a database/dataset with its tables fully populated.
type DatabaseInfo struct {
	Name   string             `json:"name"`
	Tables []driver.TableInfo `json:"tables"`
}

// DiscoveryResult holds the full schema discovery output.
type DiscoveryResult struct {
	Databases    []DatabaseInfo `json:"databases"`
	TotalTables  int            `json:"total_tables"`
	TotalColumns int            `json:"total_columns"`
}

// SampleQuery is an AI-generated or template-based query suggestion.
type SampleQuery struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	SQL         string `json:"sql"`
	Category    string `json:"category"`
}

// DiscoverAll performs full schema discovery: lists all databases, tables,
// and columns in one shot. Caps at 10 databases and 50 tables per database
// to stay fast.
func (a *App) DiscoverAll() (*DiscoveryResult, error) {
	a.mu.RLock()
	d := a.driver
	a.mu.RUnlock()

	if d == nil {
		return nil, fmt.Errorf("not connected")
	}

	databases, err := d.ListDatabases(a.ctx)
	if err != nil {
		return nil, fmt.Errorf("list databases: %w", err)
	}

	const maxDatabases = 10
	const maxTablesPerDB = 50
	const maxTotalTables = 500

	result := &DiscoveryResult{}
	totalTables := 0

	for i, dbName := range databases {
		if i >= maxDatabases {
			break
		}

		tables, err := d.ListTables(a.ctx, dbName)
		if err != nil {
			continue
		}

		dbInfo := DatabaseInfo{Name: dbName}

		for j, t := range tables {
			if j >= maxTablesPerDB || totalTables >= maxTotalTables {
				break
			}

			// Fetch full schema with columns
			info, err := d.GetTableSchema(a.ctx, dbName, t.Name)
			if err != nil {
				// Fall back to table info without columns
				dbInfo.Tables = append(dbInfo.Tables, t)
			} else {
				dbInfo.Tables = append(dbInfo.Tables, *info)
				result.TotalColumns += len(info.Columns)
			}
			totalTables++
		}

		result.Databases = append(result.Databases, dbInfo)
	}

	result.TotalTables = totalTables
	return result, nil
}

// GenerateSampleQueries uses AI (if available) or templates to create
// sample queries based on the discovered schema.
func (a *App) GenerateSampleQueries(discoveryJSON string) ([]SampleQuery, error) {
	var discovery DiscoveryResult
	if err := json.Unmarshal([]byte(discoveryJSON), &discovery); err != nil {
		return nil, fmt.Errorf("parse discovery: %w", err)
	}

	// Collect all tables with size info
	var allTables []tableRef
	for _, db := range discovery.Databases {
		for _, t := range db.Tables {
			allTables = append(allTables, tableRef{db: db.Name, table: t})
		}
	}

	// Sort tables: prefer mid-size tables that are interesting but cheap to query.
	// Avoid the biggest tables (expensive full scans) and the smallest (boring).
	// Score: tables with 1K-1M rows score highest; >10M rows get penalized.
	for i := 0; i < len(allTables); i++ {
		for j := i + 1; j < len(allTables); j++ {
			scoreI := tableCostScore(allTables[i].table)
			scoreJ := tableCostScore(allTables[j].table)
			if scoreJ > scoreI {
				allTables[i], allTables[j] = allTables[j], allTables[i]
			}
		}
	}

	topN := 8
	if len(allTables) < topN {
		topN = len(allTables)
	}
	topTables := allTables[:topN]

	a.mu.RLock()
	dType := a.connType
	a.mu.RUnlock()

	// Try AI-generated queries first
	if a.aiProv != nil && a.aiProv.IsConfigured() {
		queries, err := a.generateQueriesWithAI(topTables, dType)
		if err == nil && len(queries) > 0 {
			return queries, nil
		}
		// Fall through to templates on AI failure
	}

	// Fallback: generate simple template queries
	return a.generateTemplateQueries(topTables), nil
}

func (a *App) generateQueriesWithAI(tables []tableRef, driverType driver.DriverType) ([]SampleQuery, error) {
	// Build a compact schema description with size warnings
	var sb strings.Builder
	sb.WriteString("Tables available:\n")
	for _, t := range tables {
		sizeLabel := ""
		if t.table.SizeBytes > 0 {
			gb := float64(t.table.SizeBytes) / (1024 * 1024 * 1024)
			if gb >= 1 {
				sizeLabel = fmt.Sprintf(", %.1f GB", gb)
			} else {
				mb := float64(t.table.SizeBytes) / (1024 * 1024)
				sizeLabel = fmt.Sprintf(", %.0f MB", mb)
			}
		}
		sb.WriteString(fmt.Sprintf("- %s.%s (%d rows%s", t.db, t.table.Name, t.table.RowCount, sizeLabel))
		if len(t.table.Columns) > 0 {
			sb.WriteString(", columns: ")
			colNames := make([]string, 0, len(t.table.Columns))
			for _, c := range t.table.Columns {
				colNames = append(colNames, c.Name+" "+c.Type)
			}
			sb.WriteString(strings.Join(colNames, ", "))
		}
		sb.WriteString(")\n")
	}

	// Build cost-conscious prompt based on driver type
	costRules := ""
	if driverType == driver.BigQuery {
		costRules = `
CRITICAL COST RULES (BigQuery charges per byte scanned):
- EVERY query MUST have LIMIT 100 or less
- NEVER use SELECT * on tables larger than 100 MB — select specific columns instead
- For tables with date/timestamp partition columns, ALWAYS add a WHERE filter on the partition column (e.g. last 7 days)
- For large tables (>1 GB), keep queries simple: filter + aggregate on specific columns, never full table scans
- Prefer COUNT, SUM, AVG aggregations over raw row retrieval for large tables
- Use backtick-quoted table names: ` + "`dataset.table`" + `
- Each query should scan UNDER 1 GB of data`
	} else {
		costRules = `
IMPORTANT RULES:
- EVERY query MUST have LIMIT 100 or less
- For large tables, prefer aggregations (COUNT, SUM, AVG) over raw row retrieval
- Select specific columns instead of SELECT * when possible
- Keep queries fast and lightweight — these are sample/exploration queries`
	}

	prompt := fmt.Sprintf(`Generate exactly 5 useful, COST-EFFICIENT sample queries for a data analyst. Return ONLY a JSON array with objects having keys: title, description, sql, category.

Categories should be one of: "explore", "aggregate", "trend", "top-n", "join".
%s

%s

Return valid JSON only, no markdown.`, costRules, sb.String())

	model, _ := a.store.GetSetting("ai_model")

	resp, err := a.aiProv.Complete(a.ctx, []ai.Message{
		{Role: "user", Content: prompt},
	}, nil, model)
	if err != nil {
		return nil, err
	}

	// Parse the JSON response
	resp = strings.TrimSpace(resp)
	// Strip markdown code fences if present
	if strings.HasPrefix(resp, "```") {
		lines := strings.Split(resp, "\n")
		if len(lines) > 2 {
			resp = strings.Join(lines[1:len(lines)-1], "\n")
		}
	}

	var queries []SampleQuery
	if err := json.Unmarshal([]byte(resp), &queries); err != nil {
		return nil, fmt.Errorf("parse AI response: %w", err)
	}
	return queries, nil
}

type tableRef struct {
	db    string
	table driver.TableInfo
}

// tableCostScore ranks tables for sample query generation.
// Prefers mid-size tables (1K-1M rows) that are interesting but cheap to query.
// Penalizes very large tables (expensive scans) and very small ones (less useful).
func tableCostScore(t driver.TableInfo) int {
	rows := t.RowCount
	sizeGB := float64(t.SizeBytes) / (1024 * 1024 * 1024)

	// Penalize massive tables (>10GB = likely multi-TB scan in BQ)
	if sizeGB > 10 {
		return 1
	}
	if sizeGB > 1 {
		return 3
	}

	// Sweet spot: tables with meaningful data but manageable size
	switch {
	case rows >= 1000 && rows <= 1000000:
		return 10 // ideal range
	case rows > 1000000 && rows <= 10000000:
		return 7 // still good
	case rows > 10000000:
		return 3 // large, less ideal for samples
	case rows >= 100 && rows < 1000:
		return 5 // small but usable
	default:
		return 2 // very small or empty
	}
}

func (a *App) generateTemplateQueries(tables []tableRef) []SampleQuery {
	var queries []SampleQuery

	a.mu.RLock()
	dType := a.connType
	a.mu.RUnlock()

	for i, t := range tables {
		if i >= 5 {
			break
		}

		var fullName string
		switch dType {
		case driver.BigQuery:
			fullName = fmt.Sprintf("`%s.%s`", t.db, t.table.Name)
		default:
			if t.db != "" {
				fullName = fmt.Sprintf("%s.%s", t.db, t.table.Name)
			} else {
				fullName = t.table.Name
			}
		}

		sizeGB := float64(t.table.SizeBytes) / (1024 * 1024 * 1024)

		// For large tables, generate a count query instead of SELECT *
		if sizeGB > 1 || t.table.RowCount > 10000000 {
			queries = append(queries, SampleQuery{
				Title:       fmt.Sprintf("Count %s", t.table.Name),
				Description: fmt.Sprintf("Row count for %s (large table: %.1f GB — full scan would be expensive)", t.table.Name, sizeGB),
				SQL:         fmt.Sprintf("SELECT COUNT(*) AS total_rows\nFROM %s\nLIMIT 1", fullName),
				Category:    "aggregate",
			})
		} else {
			queries = append(queries, SampleQuery{
				Title:       fmt.Sprintf("Explore %s", t.table.Name),
				Description: fmt.Sprintf("Preview the first 100 rows from %s (%d total rows)", t.table.Name, t.table.RowCount),
				SQL:         fmt.Sprintf("SELECT *\nFROM %s\nLIMIT 100", fullName),
				Category:    "explore",
			})
		}
	}

	return queries
}

// =========================================================================
// Helpers
// =========================================================================

func (a *App) buildSchemaContext() *ai.SchemaContext {
	sc := &ai.SchemaContext{
		DriverType: string(a.connType),
		Tables:     make(map[string][]ai.SchemaColumn),
	}

	databases, err := a.driver.ListDatabases(a.ctx)
	if err != nil || len(databases) == 0 {
		return sc
	}
	sc.Database = databases[0]

	// Only load first database's tables to keep context small
	tables, err := a.driver.ListTables(a.ctx, databases[0])
	if err != nil {
		return sc
	}

	for _, t := range tables {
		if t.Columns != nil {
			cols := make([]ai.SchemaColumn, len(t.Columns))
			for i, c := range t.Columns {
				cols[i] = ai.SchemaColumn{Name: c.Name, Type: c.Type}
			}
			sc.Tables[t.Name] = cols
		} else {
			// Fetch schema for tables that don't have columns loaded
			info, err := a.driver.GetTableSchema(a.ctx, databases[0], t.Name)
			if err == nil && info != nil {
				cols := make([]ai.SchemaColumn, len(info.Columns))
				for i, c := range info.Columns {
					cols[i] = ai.SchemaColumn{Name: c.Name, Type: c.Type}
				}
				sc.Tables[t.Name] = cols
			}
		}
	}

	// Include code context from linked repos
	if len(a.codePaths) > 0 {
		codeCtx, err := a.ScanCodeContext(a.codePaths)
		if err == nil {
			for _, s := range codeCtx.Snippets {
				sc.CodeSnippets = append(sc.CodeSnippets, ai.CodeSnippetRef{
					FilePath: s.FilePath,
					Language: s.Language,
					Content:  s.Content,
					LineNum:  s.LineNum,
				})
			}
			for _, dc := range codeCtx.DetectedConnections {
				sc.DetectedConnections = append(sc.DetectedConnections, ai.DetectedConnectionRef{
					Source:     dc.Source,
					DriverHint: dc.DriverHint,
					Detail:     dc.Detail,
				})
			}
		}
	}

	return sc
}

func (a *App) loadAISettings() {
	if a.store == nil {
		return
	}
	provName, _ := a.store.GetSetting("ai_provider")
	apiKey, _ := a.store.GetSecretSetting("ai_api_key")
	endpoint, _ := a.store.GetSetting("ai_endpoint")

	if provName == "" {
		// Check env vars as fallback
		if key := os.Getenv("OPENAI_API_KEY"); key != "" {
			provName = "openai"
			apiKey = key
		} else if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
			provName = "anthropic"
			apiKey = key
		}
	}

	if provName != "" {
		prov, err := ai.New(provName, apiKey, endpoint)
		if err == nil {
			a.aiProv = prov
		}
	}
}

// GetLogPath returns the path to the application log file.
func (a *App) GetLogPath() string {
	return logger.Path()
}

// splitHostPort splits a host string into host and port parts.
// If no port is present, defaultP is used.
func splitHostPort(hostStr, defaultP string) (string, string) {
	if hostStr == "" {
		return "127.0.0.1", defaultP
	}
	host, port, err := net.SplitHostPort(hostStr)
	if err != nil {
		// No port in string
		return hostStr, defaultP
	}
	if port == "" {
		port = defaultP
	}
	return host, port
}

// defaultPort returns the default database port for a driver type.
func defaultPort(dt driver.DriverType) string {
	switch dt {
	case driver.Postgres:
		return "5432"
	case driver.MySQL:
		return "3306"
	case driver.MongoDB:
		return "27017"
	case driver.ClickHouse:
		return "9000"
	default:
		return "5432"
	}
}

func userDataDir() string {
	if dir, err := os.UserConfigDir(); err == nil {
		return dir
	}
	home, _ := os.UserHomeDir()
	return home
}

func csvEscape(s string) string {
	needsQuote := false
	for _, c := range s {
		if c == ',' || c == '"' || c == '\n' || c == '\r' {
			needsQuote = true
			break
		}
	}
	if !needsQuote {
		return s
	}
	escaped := ""
	for _, c := range s {
		if c == '"' {
			escaped += `""`
		} else {
			escaped += string(c)
		}
	}
	return `"` + escaped + `"`
}
