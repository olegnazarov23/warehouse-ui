import { writable, derived, get } from "svelte/store";
import type { EditorTab, QueryResult, DryRunResult, ExplainResult } from "../types";

let nextTabId = 1;

function createTab(title?: string, sql?: string): EditorTab {
  const id = `tab-${nextTabId++}`;
  return {
    id,
    title: title ?? `Query ${nextTabId - 1}`,
    sql: sql ?? "",
    dirty: false,
    running: false,
  };
}

export interface EditorState {
  tabs: EditorTab[];
  activeTabId: string;
}

const initial = createTab();

export const editor = writable<EditorState>({
  tabs: [initial],
  activeTabId: initial.id,
});

export const activeTab = derived(editor, ($e) =>
  $e.tabs.find((t) => t.id === $e.activeTabId)
);

// --- Tab persistence per connection ---

interface SavedTab {
  title: string;
  sql: string;
}

/** Save current tabs to localStorage for a connection */
export function saveTabs(connectionId: string) {
  if (!connectionId) return;
  const state = get(editor);
  const saved: SavedTab[] = state.tabs
    .filter((t) => t.sql.trim()) // only save tabs with content
    .map((t) => ({ title: t.title, sql: t.sql }));
  try {
    localStorage.setItem(`editor_tabs:${connectionId}`, JSON.stringify(saved));
  } catch {
    // localStorage full or unavailable
  }
}

/** Restore tabs from localStorage for a connection */
export function restoreTabs(connectionId: string) {
  if (!connectionId) return;
  try {
    const raw = localStorage.getItem(`editor_tabs:${connectionId}`);
    if (!raw) return;
    const saved: SavedTab[] = JSON.parse(raw);
    if (!saved?.length) return;

    const tabs = saved.map((s) => createTab(s.title, s.sql));
    editor.set({ tabs, activeTabId: tabs[0].id });
  } catch {
    // corrupted data, ignore
  }
}

/** Reset to a fresh tab (for new connections with no saved state) */
export function resetTabs() {
  nextTabId = 1;
  const fresh = createTab();
  editor.set({ tabs: [fresh], activeTabId: fresh.id });
}

// --- Tab operations ---

export function addTab(title?: string, sql?: string) {
  const tab = createTab(title, sql);
  editor.update((e) => ({
    tabs: [...e.tabs, tab],
    activeTabId: tab.id,
  }));
  return tab.id;
}

export function closeTab(id: string) {
  editor.update((e) => {
    const remaining = e.tabs.filter((t) => t.id !== id);
    if (remaining.length === 0) {
      const fresh = createTab();
      return { tabs: [fresh], activeTabId: fresh.id };
    }
    const activeId =
      e.activeTabId === id ? remaining[remaining.length - 1].id : e.activeTabId;
    return { tabs: remaining, activeTabId: activeId };
  });
}

export function setActiveTab(id: string) {
  editor.update((e) => ({ ...e, activeTabId: id }));
}

export function updateTabSQL(id: string, sql: string) {
  editor.update((e) => ({
    ...e,
    tabs: e.tabs.map((t) =>
      t.id === id ? { ...t, sql, dirty: true } : t
    ),
  }));
}

export function setTabResult(id: string, result: QueryResult) {
  editor.update((e) => ({
    ...e,
    tabs: e.tabs.map((t) =>
      t.id === id ? { ...t, result, error: undefined, running: false } : t
    ),
  }));
}

export function setTabDryRun(id: string, dryRun: DryRunResult) {
  editor.update((e) => ({
    ...e,
    tabs: e.tabs.map((t) =>
      t.id === id ? { ...t, dryRun } : t
    ),
  }));
}

export function setTabExplain(id: string, explain: ExplainResult) {
  editor.update((e) => ({
    ...e,
    tabs: e.tabs.map((t) =>
      t.id === id ? { ...t, explain } : t
    ),
  }));
}

export function setTabError(id: string, error: string) {
  editor.update((e) => ({
    ...e,
    tabs: e.tabs.map((t) =>
      t.id === id ? { ...t, error, running: false } : t
    ),
  }));
}

export function renameTab(id: string, title: string) {
  editor.update((e) => ({
    ...e,
    tabs: e.tabs.map((t) =>
      t.id === id ? { ...t, title } : t
    ),
  }));
}

export function setTabRunning(id: string, running: boolean) {
  editor.update((e) => ({
    ...e,
    tabs: e.tabs.map((t) =>
      t.id === id ? { ...t, running } : t
    ),
  }));
}
