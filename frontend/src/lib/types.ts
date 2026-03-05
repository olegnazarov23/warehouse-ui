// ── Driver / Connection ──────────────────────────────────────────────

export type DriverType =
  | "bigquery"
  | "postgres"
  | "mysql"
  | "mongodb"
  | "sqlite"
  | "clickhouse";

export interface ConnectionConfig {
  id: string;
  type: DriverType;
  name: string;
  host: string;
  database: string;
  username: string;
  password: string;
  ssl_mode: string;
  options: Record<string, string>;
}

export interface ConnectionStatus {
  connected: boolean;
  id: string;
  name: string;
  driver_type: DriverType;
  database: string;
}

export interface SavedConnection {
  id: string;
  name: string;
  driver_type: string;
  config_json: string;
  created_at: string;
}

// ── Schema ───────────────────────────────────────────────────────────

export interface Column {
  name: string;
  type: string;
  nullable: boolean;
  description?: string;
  is_primary?: boolean;
}

export interface TableInfo {
  name: string;
  type: string;
  row_count: number;
  size_bytes: number;
  columns?: Column[];
}

// ── Query ────────────────────────────────────────────────────────────

export interface QueryResult {
  columns: string[];
  column_types?: string[];
  rows: unknown[][];
  row_count: number;
  total_rows: number;
  duration_ms: number;
  bytes_processed?: number;
  bytes_billed?: number;
  cost_usd?: number;
  cache_hit?: boolean;
}

export interface DryRunResult {
  valid: boolean;
  estimated_bytes: number;
  estimated_cost_usd: number;
  estimated_rows: number;
  statement_type?: string;
  error?: string;
  warnings?: string[];
  referenced_tables?: string[];
}

export interface OptimizeResult {
  original_sql: string;
  optimized_sql: string;
  explanation: string;
  iterations: number;
  original_dry_run?: DryRunResult;
  optimized_dry_run?: DryRunResult;
  improvements: string[];
}

// ── Explain ─────────────────────────────────────────────────────────

export interface ExplainNode {
  operation: string;
  details?: string;
  table?: string;
  estimated_rows?: number;
  cost?: number;
  children?: ExplainNode[];
}

export interface ExplainResult {
  plan: ExplainNode;
  raw_text: string;
}

// ── History ──────────────────────────────────────────────────────────

export interface HistoryEntry {
  id: string;
  connection_id: string;
  connection_name: string;
  driver_type: string;
  sql: string;
  status: string;
  error: string;
  row_count?: number;
  duration_ms?: number;
  bytes_processed?: number;
  cost_usd?: number;
  cache_hit: boolean;
  created_at: string;
}

export interface HistoryStats {
  total_queries: number;
  total_bytes: number;
  total_cost: number;
  avg_duration_ms: number;
}

// ── Saved Queries ────────────────────────────────────────────────────

export interface SavedQuery {
  id: string;
  name: string;
  description: string;
  sql: string;
  tags: string;
  slug: string;
  connection_id: string;
  created_at: string;
  updated_at: string;
}

// ── Templates ────────────────────────────────────────────────────────

export interface QueryTemplate {
  id: string;
  name: string;
  description: string;
  sql: string;
  driver_type: string;
  category: string;
}

// ── AI ───────────────────────────────────────────────────────────────

export interface ProviderInfo {
  name: string;
  default_model: string;
  min_model: string;
  configured: boolean;
}

export interface AiSettings {
  provider: string;
  model: string;
  api_key: string;
  endpoint: string;
}

export interface ChatMessage {
  role: "user" | "assistant";
  content: string;
  timestamp: number;
}

export interface Conversation {
  id: string;
  connection_id: string;
  title: string;
  created_at: string;
  updated_at: string;
}

export interface StoredChatMessage {
  id: string;
  conversation_id: string;
  role: string;
  content: string;
  created_at: string;
}

// ── Editor Tabs ──────────────────────────────────────────────────────

export interface EditorTab {
  id: string;
  title: string;
  sql: string;
  dirty: boolean;
  result?: QueryResult;
  dryRun?: DryRunResult;
  explain?: ExplainResult;
  error?: string;
  running: boolean;
}

// ── Discovery ─────────────────────────────────────────────────────────

export interface DatabaseInfo {
  name: string;
  tables: TableInfo[];
}

export interface DiscoveryResult {
  databases: DatabaseInfo[];
  total_tables: number;
  total_columns: number;
}

export interface SampleQuery {
  title: string;
  description: string;
  sql: string;
  category: string;
}

// ── UI State ─────────────────────────────────────────────────────────

export type AppView = "connect" | "workspace";
export type SidebarTab = "schema" | "saved" | "history" | "templates";
