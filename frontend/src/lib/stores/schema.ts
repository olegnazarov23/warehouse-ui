import { writable, derived } from "svelte/store";
import type { TableInfo, Column } from "../types";

export interface SchemaState {
  databases: string[];
  activeDatabase: string;
  tables: TableInfo[];
  expandedTables: Set<string>;
  tableColumns: Record<string, Column[]>;
  loading: boolean;
}

export const schema = writable<SchemaState>({
  databases: [],
  activeDatabase: "",
  tables: [],
  expandedTables: new Set(),
  tableColumns: {},
  loading: false,
});

export const activeDatabase = derived(schema, ($s) => $s.activeDatabase);
export const schemaTables = derived(schema, ($s) => $s.tables);
export const schemaLoading = derived(schema, ($s) => $s.loading);

export function toggleTableExpanded(tableName: string) {
  schema.update((s) => {
    const next = new Set(s.expandedTables);
    if (next.has(tableName)) {
      next.delete(tableName);
    } else {
      next.add(tableName);
    }
    return { ...s, expandedTables: next };
  });
}

export function setTableColumns(tableName: string, columns: Column[]) {
  schema.update((s) => ({
    ...s,
    tableColumns: { ...s.tableColumns, [tableName]: columns },
  }));
}

export function resetSchema() {
  schema.set({
    databases: [],
    activeDatabase: "",
    tables: [],
    expandedTables: new Set(),
    tableColumns: {},
    loading: false,
  });
}
