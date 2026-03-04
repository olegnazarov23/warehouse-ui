/**
 * API abstraction layer.
 *
 * In desktop mode (Wails) the functions are exposed on `window.go.main.App`.
 * In server mode they would come from fetch(). This module wraps both so
 * the rest of the app doesn't care.
 */

import type {
  AiSettings,
  ConnectionConfig,
  ConnectionStatus,
  Conversation,
  DiscoveryResult,
  DryRunResult,
  HistoryEntry,
  HistoryStats,
  OptimizeResult,
  ProviderInfo,
  QueryResult,
  QueryTemplate,
  SampleQuery,
  SavedConnection,
  SavedQuery,
  StoredChatMessage,
  TableInfo,
} from "./types";

// Wails injects `window.go` at runtime.
// During dev the bindings live under wailsjs/go/main/App.
// We dynamically import to avoid hard failures in server mode.

let wailsApp: any = null;

async function getApp(): Promise<any> {
  if (wailsApp) return wailsApp;

  // Desktop mode: Wails runtime
  if ((window as any).go?.main?.App) {
    wailsApp = (window as any).go.main.App;
    return wailsApp;
  }

  // Try dynamic import of generated Wails bindings
  try {
    const mod = await import("../../wailsjs/go/main/App.js");
    wailsApp = mod;
    return wailsApp;
  } catch {
    throw new Error("No backend available. Are you running inside Wails?");
  }
}

// ── Connection ───────────────────────────────────────────────────────

export async function connect(cfg: ConnectionConfig): Promise<ConnectionStatus> {
  const app = await getApp();
  return app.Connect(cfg);
}

export async function disconnect(): Promise<void> {
  const app = await getApp();
  return app.Disconnect();
}

export async function getConnectionStatus(): Promise<ConnectionStatus> {
  const app = await getApp();
  return app.GetConnectionStatus();
}

export async function getAvailableDrivers(): Promise<string[]> {
  const app = await getApp();
  return app.GetAvailableDrivers();
}

export async function pickFile(title: string): Promise<string> {
  const app = await getApp();
  return app.PickFile(title);
}

export async function pickFolder(title: string): Promise<string> {
  const app = await getApp();
  return app.PickFolder(title);
}

export async function setCodePaths(paths: string[]): Promise<void> {
  const app = await getApp();
  return app.SetCodePaths(paths);
}

export async function getCodePaths(): Promise<string[]> {
  const app = await getApp();
  return app.GetCodePaths();
}

export async function scanCodeContext(paths: string[]): Promise<any> {
  const app = await getApp();
  return app.ScanCodeContext(paths);
}

export async function parseConnectionString(
  connStr: string,
  driverHint: string
): Promise<ConnectionConfig> {
  const app = await getApp();
  return app.ParseConnectionString(connStr, driverHint);
}

export async function testConnection(cfg: ConnectionConfig): Promise<void> {
  const app = await getApp();
  return app.TestConnection(cfg);
}

export async function listServerDatabases(cfg: ConnectionConfig): Promise<string[]> {
  const app = await getApp();
  return app.ListServerDatabases(cfg);
}

// ── Saved Connections ────────────────────────────────────────────────

export async function listSavedConnections(): Promise<SavedConnection[]> {
  const app = await getApp();
  return app.ListSavedConnections();
}

export async function saveConnection(cfg: ConnectionConfig): Promise<void> {
  const app = await getApp();
  return app.SaveConnection(cfg);
}

export async function deleteConnection(id: string): Promise<void> {
  const app = await getApp();
  return app.DeleteConnection(id);
}

// ── Schema ───────────────────────────────────────────────────────────

export async function listDatabases(): Promise<string[]> {
  const app = await getApp();
  return app.ListDatabases();
}

export async function listTables(database: string): Promise<TableInfo[]> {
  const app = await getApp();
  return app.ListTables(database);
}

export async function getTableSchema(
  database: string,
  table: string
): Promise<TableInfo> {
  const app = await getApp();
  return app.GetTableSchema(database, table);
}

export async function previewTable(
  database: string,
  table: string,
  limit: number
): Promise<QueryResult> {
  const app = await getApp();
  return app.PreviewTable(database, table, limit);
}

// ── Query Execution ──────────────────────────────────────────────────

export async function dryRun(sql: string): Promise<DryRunResult> {
  const app = await getApp();
  return app.DryRun(sql);
}

export async function execute(sql: string, limit: number): Promise<QueryResult> {
  const app = await getApp();
  return app.Execute(sql, limit);
}

export async function cancelQuery(): Promise<void> {
  const app = await getApp();
  return app.CancelQuery();
}

// ── History ──────────────────────────────────────────────────────────

export async function getHistory(
  search: string,
  limit: number,
  offset: number
): Promise<HistoryEntry[]> {
  const app = await getApp();
  return app.GetHistory(search, limit, offset);
}

export async function deleteHistory(id: string): Promise<void> {
  const app = await getApp();
  return app.DeleteHistory(id);
}

export async function clearHistory(): Promise<void> {
  const app = await getApp();
  return app.ClearHistory();
}

export async function getHistoryStats(): Promise<HistoryStats> {
  const app = await getApp();
  return app.GetHistoryStats();
}

// ── Saved Queries ────────────────────────────────────────────────────

export async function listSavedQueries(search: string): Promise<SavedQuery[]> {
  const app = await getApp();
  return app.ListSavedQueries(search);
}

export async function saveQuery(
  name: string,
  sql: string,
  description: string,
  tagsJSON: string
): Promise<SavedQuery> {
  const app = await getApp();
  return app.SaveQuery(name, sql, description, tagsJSON);
}

export async function deleteSavedQuery(id: string): Promise<void> {
  const app = await getApp();
  return app.DeleteSavedQuery(id);
}

export async function getQueryBySlug(slug: string): Promise<SavedQuery> {
  const app = await getApp();
  return app.GetQueryBySlug(slug);
}

// ── Templates ────────────────────────────────────────────────────────

export async function listTemplates(
  driverType: string
): Promise<QueryTemplate[]> {
  const app = await getApp();
  return app.ListTemplates(driverType);
}

// ── AI ───────────────────────────────────────────────────────────────

export async function aiChat(
  message: string,
  conversationID: string,
  editorContext: string = ""
): Promise<void> {
  const app = await getApp();
  return app.AiChat(message, conversationID, editorContext);
}

export async function getAiProviders(): Promise<ProviderInfo[]> {
  const app = await getApp();
  return app.GetAiProviders();
}

export async function getAiSettings(): Promise<AiSettings> {
  const app = await getApp();
  return app.GetAiSettings();
}

export async function setAiSettings(
  provider: string,
  model: string,
  apiKey: string,
  endpoint: string
): Promise<void> {
  const app = await getApp();
  return app.SetAiSettings(provider, model, apiKey, endpoint);
}

// ── AI Conversations ─────────────────────────────────────────────

export async function listAiConversations(): Promise<Conversation[]> {
  const app = await getApp();
  return app.ListAiConversations();
}

export async function getAiMessages(
  conversationID: string
): Promise<StoredChatMessage[]> {
  const app = await getApp();
  return app.GetAiMessages(conversationID);
}

export async function deleteAiConversation(id: string): Promise<void> {
  const app = await getApp();
  return app.DeleteAiConversation(id);
}

export async function renameAiConversation(
  id: string,
  title: string
): Promise<void> {
  const app = await getApp();
  return app.RenameAiConversation(id, title);
}

// ── AI Optimizer ─────────────────────────────────────────────────

export async function optimizeQuery(sql: string): Promise<OptimizeResult> {
  const app = await getApp();
  return app.OptimizeQuery(sql);
}

// ── Discovery ────────────────────────────────────────────────────

export async function discoverAll(): Promise<DiscoveryResult> {
  const app = await getApp();
  return app.DiscoverAll();
}

export async function generateSampleQueries(
  discoveryJSON: string
): Promise<SampleQuery[]> {
  const app = await getApp();
  return app.GenerateSampleQueries(discoveryJSON);
}

// ── Cache ─────────────────────────────────────────────────────────────

export async function clearQueryCache(): Promise<void> {
  const app = await getApp();
  return app.ClearQueryCache();
}

// ── Export ────────────────────────────────────────────────────────────

export async function exportCSV(sql: string): Promise<string> {
  const app = await getApp();
  return app.ExportCSV(sql);
}

export async function exportJSON(sql: string): Promise<string> {
  const app = await getApp();
  return app.ExportJSON(sql);
}

export async function exportExcel(sql: string): Promise<string> {
  const app = await getApp();
  return app.ExportExcel(sql);
}
