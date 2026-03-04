package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Store manages the local SQLite database for connections, history, and settings.
type Store struct {
	db   *sql.DB
	path string
}

// New creates and initializes a Store at the given directory.
// The SQLite database file will be created at dir/warehouse_ui.db.
func New(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	dbPath := filepath.Join(dir, "warehouse_ui.db")
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}

	s := &Store{db: db, path: dbPath}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

// Close closes the underlying database.
func (s *Store) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

func (s *Store) migrate() error {
	migrations := []string{
		`CREATE TABLE IF NOT EXISTS connections (
			id          TEXT PRIMARY KEY,
			name        TEXT NOT NULL,
			driver_type TEXT NOT NULL,
			config_json TEXT NOT NULL,
			created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS query_history (
			id              TEXT PRIMARY KEY,
			connection_id   TEXT,
			connection_name TEXT,
			driver_type     TEXT,
			sql_text        TEXT NOT NULL,
			status          TEXT NOT NULL DEFAULT 'completed',
			error_msg       TEXT,
			bytes_processed INTEGER,
			bytes_billed    INTEGER,
			cost_usd        REAL,
			duration_ms     INTEGER,
			row_count       INTEGER,
			cache_hit       BOOLEAN DEFAULT 0,
			created_at      DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_history_created ON query_history(created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_history_conn ON query_history(connection_id)`,
		`CREATE TABLE IF NOT EXISTS saved_queries (
			id          TEXT PRIMARY KEY,
			name        TEXT NOT NULL,
			description TEXT DEFAULT '',
			sql_text    TEXT NOT NULL,
			tags_json   TEXT DEFAULT '[]',
			slug        TEXT UNIQUE,
			connection_id TEXT,
			created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_saved_slug ON saved_queries(slug)`,
		`CREATE TABLE IF NOT EXISTS settings (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS query_templates (
			id          TEXT PRIMARY KEY,
			name        TEXT NOT NULL,
			description TEXT DEFAULT '',
			sql_text    TEXT NOT NULL,
			driver_type TEXT NOT NULL,
			category    TEXT DEFAULT 'general',
			difficulty  TEXT DEFAULT 'beginner',
			created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS ai_conversations (
			id         TEXT PRIMARY KEY,
			title      TEXT NOT NULL DEFAULT 'New Chat',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS ai_messages (
			id              TEXT PRIMARY KEY,
			conversation_id TEXT NOT NULL REFERENCES ai_conversations(id) ON DELETE CASCADE,
			role            TEXT NOT NULL,
			content         TEXT NOT NULL,
			created_at      DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_ai_messages_conv ON ai_messages(conversation_id, created_at)`,
	}

	for _, m := range migrations {
		if _, err := s.db.Exec(m); err != nil {
			return fmt.Errorf("migration: %w\nSQL: %s", err, m)
		}
	}

	// Seed built-in query templates for beginners
	s.seedTemplates()
	return nil
}

// seedTemplates inserts starter query templates for junior data engineers.
func (s *Store) seedTemplates() {
	templates := []struct {
		id, name, desc, sql, driver, category, difficulty string
	}{
		{
			"tpl_pg_table_sizes", "Table sizes (PostgreSQL)",
			"Find the largest tables in your database by total size including indexes.",
			`SELECT schemaname || '.' || tablename AS table,
       pg_size_pretty(pg_total_relation_size(schemaname || '.' || tablename)) AS total_size,
       pg_size_pretty(pg_relation_size(schemaname || '.' || tablename)) AS data_size,
       pg_size_pretty(pg_indexes_size(schemaname || '.' || tablename)) AS index_size
FROM pg_tables
WHERE schemaname NOT IN ('pg_catalog', 'information_schema')
ORDER BY pg_total_relation_size(schemaname || '.' || tablename) DESC
LIMIT 20`,
			"postgres", "Schema Exploration", "beginner",
		},
		{
			"tpl_pg_slow_queries", "Slow queries (PostgreSQL)",
			"Find the slowest queries using pg_stat_statements (requires extension).",
			`SELECT query,
       calls,
       round(total_exec_time::numeric, 2) AS total_ms,
       round(mean_exec_time::numeric, 2) AS avg_ms,
       rows
FROM pg_stat_statements
ORDER BY mean_exec_time DESC
LIMIT 20`,
			"postgres", "Performance", "intermediate",
		},
		{
			"tpl_bq_dataset_sizes", "Dataset sizes (BigQuery)",
			"See how much storage each dataset uses. Useful for cost management.",
			`SELECT table_schema AS dataset,
       COUNT(*) AS tables,
       SUM(size_bytes) / POW(1024, 3) AS total_gb,
       SUM(row_count) AS total_rows
FROM region-us.INFORMATION_SCHEMA.TABLE_STORAGE
GROUP BY table_schema
ORDER BY total_gb DESC`,
			"bigquery", "Schema Exploration", "beginner",
		},
		{
			"tpl_bq_expensive_tables", "Most expensive tables (BigQuery)",
			"Find the largest tables that cost the most to query. At $5/TB, a 1TB table costs $5 per full scan.",
			`SELECT table_name,
       ROUND(size_bytes / POW(1024, 4), 4) AS size_tb,
       ROUND(size_bytes / POW(1024, 4) * 5, 2) AS full_scan_cost_usd,
       row_count
FROM region-us.INFORMATION_SCHEMA.TABLE_STORAGE
WHERE table_schema = 'your_dataset'
ORDER BY size_bytes DESC
LIMIT 20`,
			"bigquery", "Cost Management", "beginner",
		},
		{
			"tpl_mysql_table_sizes", "Table sizes (MySQL)",
			"Find the largest tables in your MySQL database.",
			`SELECT TABLE_NAME AS table_name,
       TABLE_ROWS AS rows,
       ROUND(DATA_LENGTH / 1024 / 1024, 2) AS data_mb,
       ROUND(INDEX_LENGTH / 1024 / 1024, 2) AS index_mb,
       ROUND((DATA_LENGTH + INDEX_LENGTH) / 1024 / 1024, 2) AS total_mb
FROM information_schema.TABLES
WHERE TABLE_SCHEMA = DATABASE()
ORDER BY (DATA_LENGTH + INDEX_LENGTH) DESC
LIMIT 20`,
			"mysql", "Schema Exploration", "beginner",
		},
		{
			"tpl_ch_parts_info", "Table parts (ClickHouse)",
			"See how data is distributed across parts for each table.",
			`SELECT database, table,
       count() AS parts,
       sum(rows) AS total_rows,
       formatReadableSize(sum(bytes_on_disk)) AS disk_size
FROM system.parts
WHERE active
GROUP BY database, table
ORDER BY sum(bytes_on_disk) DESC
LIMIT 20`,
			"clickhouse", "Schema Exploration", "beginner",
		},
	}

	for _, t := range templates {
		s.db.Exec(`INSERT OR IGNORE INTO query_templates (id, name, description, sql_text, driver_type, category, difficulty)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			t.id, t.name, t.desc, t.sql, t.driver, t.category, t.difficulty)
	}
}

// ---------------------------------------------------------------------------
// Settings
// ---------------------------------------------------------------------------

// GetSetting retrieves a setting value by key.
func (s *Store) GetSetting(key string) (string, error) {
	var value string
	err := s.db.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

// SetSetting saves a setting value.
func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(
		"INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = ?",
		key, value, value)
	return err
}

// deriveKey produces a 32-byte AES key from the database path + a stable salt.
// This ties encrypted data to this specific database file on this machine.
func (s *Store) deriveKey() []byte {
	h := sha256.Sum256([]byte("warehouse-ui:" + s.path))
	return h[:]
}

func (s *Store) encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	block, err := aes.NewCipher(s.deriveKey())
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	ct := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return "enc:" + base64.StdEncoding.EncodeToString(ct), nil
}

func (s *Store) decrypt(encoded string) (string, error) {
	if encoded == "" {
		return "", nil
	}
	// Support reading unencrypted legacy values
	if !strings.HasPrefix(encoded, "enc:") {
		return encoded, nil
	}
	data, err := base64.StdEncoding.DecodeString(encoded[4:])
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(s.deriveKey())
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	ns := gcm.NonceSize()
	if len(data) < ns {
		return "", fmt.Errorf("ciphertext too short")
	}
	plaintext, err := gcm.Open(nil, data[:ns], data[ns:], nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

// SetSecretSetting stores an encrypted setting value.
func (s *Store) SetSecretSetting(key, value string) error {
	enc, err := s.encrypt(value)
	if err != nil {
		return fmt.Errorf("encrypt setting: %w", err)
	}
	return s.SetSetting(key, enc)
}

// GetSecretSetting retrieves and decrypts a setting value.
func (s *Store) GetSecretSetting(key string) (string, error) {
	raw, err := s.GetSetting(key)
	if err != nil || raw == "" {
		return raw, err
	}
	return s.decrypt(raw)
}

// ---------------------------------------------------------------------------
// Connections
// ---------------------------------------------------------------------------

// SavedConnection represents a stored database connection config.
type SavedConnection struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	DriverType string `json:"driver_type"`
	ConfigJSON string `json:"config_json"`
	CreatedAt  string `json:"created_at"`
}

func (s *Store) ListConnections() ([]SavedConnection, error) {
	rows, err := s.db.Query("SELECT id, name, driver_type, config_json, created_at FROM connections ORDER BY updated_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var conns []SavedConnection
	for rows.Next() {
		var c SavedConnection
		if err := rows.Scan(&c.ID, &c.Name, &c.DriverType, &c.ConfigJSON, &c.CreatedAt); err != nil {
			return nil, err
		}
		// Decrypt config_json (contains passwords, SA JSON paths, etc.)
		if dec, err := s.decrypt(c.ConfigJSON); err == nil {
			c.ConfigJSON = dec
		}
		conns = append(conns, c)
	}
	return conns, rows.Err()
}

func (s *Store) SaveConnection(id, name, driverType, configJSON string) error {
	// Encrypt config_json — contains passwords and credentials
	enc, err := s.encrypt(configJSON)
	if err != nil {
		return fmt.Errorf("encrypt connection config: %w", err)
	}
	_, err = s.db.Exec(`INSERT INTO connections (id, name, driver_type, config_json)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET name=?, driver_type=?, config_json=?, updated_at=CURRENT_TIMESTAMP`,
		id, name, driverType, enc, name, driverType, enc)
	return err
}

func (s *Store) DeleteConnection(id string) error {
	_, err := s.db.Exec("DELETE FROM connections WHERE id = ?", id)
	return err
}

// ---------------------------------------------------------------------------
// Query History
// ---------------------------------------------------------------------------

// HistoryEntry represents a single query history record.
type HistoryEntry struct {
	ID             string   `json:"id"`
	ConnectionID   string   `json:"connection_id"`
	ConnectionName string   `json:"connection_name"`
	DriverType     string   `json:"driver_type"`
	SQL            string   `json:"sql"`
	Status         string   `json:"status"`
	Error          string   `json:"error,omitempty"`
	BytesProcessed *int64   `json:"bytes_processed,omitempty"`
	CostUSD        *float64 `json:"cost_usd,omitempty"`
	DurationMs     *int64   `json:"duration_ms,omitempty"`
	RowCount       *int64   `json:"row_count,omitempty"`
	CacheHit       bool     `json:"cache_hit"`
	CreatedAt      string   `json:"created_at"`
}

func (s *Store) AddHistory(entry HistoryEntry) error {
	_, err := s.db.Exec(`INSERT INTO query_history
		(id, connection_id, connection_name, driver_type, sql_text, status, error_msg,
		 bytes_processed, bytes_billed, cost_usd, duration_ms, row_count, cache_hit)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		entry.ID, entry.ConnectionID, entry.ConnectionName, entry.DriverType,
		entry.SQL, entry.Status, entry.Error,
		entry.BytesProcessed, nil, entry.CostUSD, entry.DurationMs, entry.RowCount, entry.CacheHit)
	return err
}

func (s *Store) ListHistory(search string, limit, offset int) ([]HistoryEntry, error) {
	if limit <= 0 {
		limit = 50
	}
	query := `SELECT id, COALESCE(connection_id,''), COALESCE(connection_name,''),
		COALESCE(driver_type,''), sql_text, status, COALESCE(error_msg,''),
		bytes_processed, cost_usd, duration_ms, row_count, cache_hit, created_at
		FROM query_history`
	args := []interface{}{}

	if search != "" {
		query += " WHERE sql_text LIKE ?"
		args = append(args, "%"+search+"%")
	}
	query += " ORDER BY created_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []HistoryEntry
	for rows.Next() {
		var e HistoryEntry
		if err := rows.Scan(&e.ID, &e.ConnectionID, &e.ConnectionName,
			&e.DriverType, &e.SQL, &e.Status, &e.Error,
			&e.BytesProcessed, &e.CostUSD, &e.DurationMs, &e.RowCount,
			&e.CacheHit, &e.CreatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

func (s *Store) DeleteHistory(id string) error {
	_, err := s.db.Exec("DELETE FROM query_history WHERE id = ?", id)
	return err
}

func (s *Store) ClearHistory() error {
	_, err := s.db.Exec("DELETE FROM query_history")
	return err
}

// ---------------------------------------------------------------------------
// Saved Queries
// ---------------------------------------------------------------------------

// SavedQuery represents a user-saved query.
type SavedQuery struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	SQL          string `json:"sql"`
	Tags         string `json:"tags"` // JSON array
	Slug         string `json:"slug"`
	ConnectionID string `json:"connection_id"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

func (s *Store) ListSavedQueries(search string) ([]SavedQuery, error) {
	query := "SELECT id, name, description, sql_text, tags_json, COALESCE(slug,''), COALESCE(connection_id,''), created_at, updated_at FROM saved_queries"
	args := []interface{}{}
	if search != "" {
		query += " WHERE name LIKE ? OR sql_text LIKE ?"
		args = append(args, "%"+search+"%", "%"+search+"%")
	}
	query += " ORDER BY updated_at DESC"

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var queries []SavedQuery
	for rows.Next() {
		var q SavedQuery
		if err := rows.Scan(&q.ID, &q.Name, &q.Description, &q.SQL, &q.Tags, &q.Slug, &q.ConnectionID, &q.CreatedAt, &q.UpdatedAt); err != nil {
			return nil, err
		}
		queries = append(queries, q)
	}
	return queries, rows.Err()
}

func (s *Store) SaveQuery(q SavedQuery) error {
	_, err := s.db.Exec(`INSERT INTO saved_queries (id, name, description, sql_text, tags_json, slug, connection_id)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET name=?, description=?, sql_text=?, tags_json=?, slug=?, updated_at=CURRENT_TIMESTAMP`,
		q.ID, q.Name, q.Description, q.SQL, q.Tags, q.Slug, q.ConnectionID,
		q.Name, q.Description, q.SQL, q.Tags, q.Slug)
	return err
}

func (s *Store) DeleteSavedQuery(id string) error {
	_, err := s.db.Exec("DELETE FROM saved_queries WHERE id = ?", id)
	return err
}

func (s *Store) GetQueryBySlug(slug string) (*SavedQuery, error) {
	var q SavedQuery
	err := s.db.QueryRow(
		"SELECT id, name, description, sql_text, tags_json, slug, COALESCE(connection_id,''), created_at, updated_at FROM saved_queries WHERE slug = ?",
		slug).Scan(&q.ID, &q.Name, &q.Description, &q.SQL, &q.Tags, &q.Slug, &q.ConnectionID, &q.CreatedAt, &q.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &q, nil
}

// ---------------------------------------------------------------------------
// Query Templates (for beginners)
// ---------------------------------------------------------------------------

// QueryTemplate is a built-in or user-created query template with guidance.
type QueryTemplate struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	SQL         string `json:"sql"`
	DriverType  string `json:"driver_type"`
	Category    string `json:"category"`
	Difficulty  string `json:"difficulty"`
}

func (s *Store) ListTemplates(driverType string) ([]QueryTemplate, error) {
	query := "SELECT id, name, description, sql_text, driver_type, category, difficulty FROM query_templates"
	args := []interface{}{}
	if driverType != "" {
		query += " WHERE driver_type = ?"
		args = append(args, driverType)
	}
	query += " ORDER BY difficulty, category, name"

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var templates []QueryTemplate
	for rows.Next() {
		var t QueryTemplate
		if err := rows.Scan(&t.ID, &t.Name, &t.Description, &t.SQL, &t.DriverType, &t.Category, &t.Difficulty); err != nil {
			return nil, err
		}
		templates = append(templates, t)
	}
	return templates, rows.Err()
}

// ---------------------------------------------------------------------------
// AI Conversations
// ---------------------------------------------------------------------------

// Conversation represents an AI chat conversation.
type Conversation struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// ChatMessage represents a single message in a conversation.
type ChatMessage struct {
	ID             string `json:"id"`
	ConversationID string `json:"conversation_id"`
	Role           string `json:"role"`
	Content        string `json:"content"`
	CreatedAt      string `json:"created_at"`
}

func (s *Store) ListConversations() ([]Conversation, error) {
	rows, err := s.db.Query("SELECT id, title, created_at, updated_at FROM ai_conversations ORDER BY updated_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var convs []Conversation
	for rows.Next() {
		var c Conversation
		if err := rows.Scan(&c.ID, &c.Title, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		convs = append(convs, c)
	}
	return convs, rows.Err()
}

func (s *Store) CreateConversation(id, title string) error {
	_, err := s.db.Exec("INSERT INTO ai_conversations (id, title) VALUES (?, ?)", id, title)
	return err
}

func (s *Store) UpdateConversationTitle(id, title string) error {
	_, err := s.db.Exec("UPDATE ai_conversations SET title = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?", title, id)
	return err
}

func (s *Store) DeleteConversation(id string) error {
	_, err := s.db.Exec("DELETE FROM ai_conversations WHERE id = ?", id)
	return err
}

func (s *Store) TouchConversation(id string) error {
	_, err := s.db.Exec("UPDATE ai_conversations SET updated_at = CURRENT_TIMESTAMP WHERE id = ?", id)
	return err
}

func (s *Store) AddChatMessage(id, conversationID, role, content string) error {
	_, err := s.db.Exec("INSERT INTO ai_messages (id, conversation_id, role, content) VALUES (?, ?, ?, ?)",
		id, conversationID, role, content)
	if err != nil {
		return err
	}
	return s.TouchConversation(conversationID)
}

func (s *Store) ListChatMessages(conversationID string) ([]ChatMessage, error) {
	rows, err := s.db.Query(
		"SELECT id, conversation_id, role, content, created_at FROM ai_messages WHERE conversation_id = ? ORDER BY created_at",
		conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []ChatMessage
	for rows.Next() {
		var m ChatMessage
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.Role, &m.Content, &m.CreatedAt); err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

// ---------------------------------------------------------------------------
// Stats (for dashboard / guidance)
// ---------------------------------------------------------------------------

// HistoryStats returns aggregate stats about query history.
type HistoryStats struct {
	TotalQueries    int     `json:"total_queries"`
	TotalCostUSD    float64 `json:"total_cost_usd"`
	TotalBytesRead  int64   `json:"total_bytes_read"`
	AvgDurationMs   int64   `json:"avg_duration_ms"`
	QueriesThisWeek int     `json:"queries_this_week"`
}

func (s *Store) GetHistoryStats() (*HistoryStats, error) {
	var stats HistoryStats

	s.db.QueryRow("SELECT COUNT(*), COALESCE(SUM(cost_usd), 0), COALESCE(SUM(bytes_processed), 0), COALESCE(AVG(duration_ms), 0) FROM query_history").
		Scan(&stats.TotalQueries, &stats.TotalCostUSD, &stats.TotalBytesRead, &stats.AvgDurationMs)

	weekAgo := time.Now().AddDate(0, 0, -7).Format("2006-01-02")
	s.db.QueryRow("SELECT COUNT(*) FROM query_history WHERE created_at >= ?", weekAgo).
		Scan(&stats.QueriesThisWeek)

	return &stats, nil
}
