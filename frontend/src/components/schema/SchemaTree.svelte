<script lang="ts">
  import { onMount } from "svelte";
  import {
    schema,
    toggleTableExpanded,
    setTableColumns,
  } from "../../lib/stores/schema";
  import { discovery, hasDiscovery } from "../../lib/stores/discovery";
  import { connectionStatus } from "../../lib/stores/connection";
  import { addTab } from "../../lib/stores/editor";
  import {
    listDatabases,
    listTables,
    getTableSchema,
    previewTable,
  } from "../../lib/api";
  import { formatBytes, formatNumber } from "../../lib/format";
  import type { TableInfo } from "../../lib/types";

  let schemaError = "";
  let manualDataset = "";
  let loadingManual = false;
  let loadingTables = false;
  let loadingColumn: string | null = null;
  let lastConnId = "";

  onMount(() => {
    lastConnId = $connectionStatus.id ?? "";
    initialLoad();
  });

  // Re-load schema when connection changes (Shell stays mounted)
  $: if ($connectionStatus.connected && $connectionStatus.id && $connectionStatus.id !== lastConnId) {
    lastConnId = $connectionStatus.id;
    initialLoad();
  }

  function initialLoad() {
    if (!$hasDiscovery) {
      loadSchema();
    } else {
      for (const db of $discovery.result!.databases) {
        for (const t of db.tables) {
          if (t.columns && t.columns.length > 0) {
            setTableColumns(t.name, t.columns);
          }
        }
      }
    }
  }

  async function loadSchema() {
    schema.update((s) => ({ ...s, loading: true }));
    schemaError = "";
    try {
      const dbs = await listDatabases();
      schema.update((s) => ({ ...s, databases: dbs ?? [] }));
      if (dbs && dbs.length > 0) {
        await selectDatabase(dbs[0]);
      } else {
        schemaError = "No datasets found. Your service account may not have permission to list datasets.";
      }
    } catch (e: any) {
      const msg = typeof e === "string" ? e : e?.message || "Unknown error";
      schemaError = `Could not list datasets: ${msg}`;
      console.error("Failed to load schema:", e);
    }
    schema.update((s) => ({ ...s, loading: false }));
  }

  async function loadManualDataset() {
    const ds = manualDataset.trim();
    if (!ds) return;
    loadingManual = true;
    schemaError = "";
    try {
      const tables = await listTables(ds);
      schema.update((s) => ({
        ...s,
        databases: [...s.databases.filter(d => d !== ds), ds],
        activeDatabase: ds,
        tables: tables ?? [],
      }));
      if (!tables || tables.length === 0) {
        schemaError = `Dataset "${ds}" has no tables, but you can still run queries against it.`;
      }
      manualDataset = "";
    } catch (e: any) {
      const msg = typeof e === "string" ? e : e?.message || "Unknown error";
      schemaError = `Could not access dataset "${ds}": ${msg}`;
    }
    loadingManual = false;
  }

  function handleManualKeydown(e: KeyboardEvent) {
    if (e.key === "Enter") {
      e.preventDefault();
      loadManualDataset();
    }
  }

  async function selectDatabase(db: string) {
    schema.update((s) => ({ ...s, activeDatabase: db, tables: [] }));
    loadingTables = true;

    if ($hasDiscovery) {
      const discoveredDB = $discovery.result!.databases.find(d => d.name === db);
      if (discoveredDB) {
        schema.update((s) => ({ ...s, tables: discoveredDB.tables }));
        for (const t of discoveredDB.tables) {
          if (t.columns && t.columns.length > 0) {
            setTableColumns(t.name, t.columns);
          }
        }
        loadingTables = false;
        return;
      }
    }

    try {
      const tables = await listTables(db);
      schema.update((s) => ({ ...s, tables: tables ?? [] }));
    } catch (e) {
      console.error("Failed to list tables:", e);
    }
    loadingTables = false;
  }

  async function expandTable(table: TableInfo) {
    toggleTableExpanded(table.name);
    if (!$schema.tableColumns[table.name]) {
      if ($hasDiscovery) {
        for (const db of $discovery.result!.databases) {
          const found = db.tables.find(t => t.name === table.name);
          if (found?.columns && found.columns.length > 0) {
            setTableColumns(table.name, found.columns);
            return;
          }
        }
      }
      loadingColumn = table.name;
      try {
        const info = await getTableSchema($schema.activeDatabase, table.name);
        if (info?.columns) {
          setTableColumns(table.name, info.columns);
        }
      } catch (e) {
        console.error("Failed to get table schema:", e);
      }
      loadingColumn = null;
    }
  }

  function insertTableName(name: string) {
    const fullName = $schema.activeDatabase
      ? `${$schema.activeDatabase}.${name}`
      : name;
    addTab(name, `SELECT *\nFROM ${fullName}\nLIMIT 100`);
  }

  let previewingTable: string | null = null;

  async function handlePreview(name: string) {
    previewingTable = name;
    try {
      const result = await previewTable($schema.activeDatabase, name, 50);
      const tab = addTab(`Preview: ${name}`);
      const { setTabResult } = await import("../../lib/stores/editor");
      setTabResult(tab, result);
    } catch (e) {
      console.error("Preview failed:", e);
    }
    previewingTable = null;
  }
</script>

<div class="p-3">
  {#if $schema.loading}
    <div class="flex items-center gap-2 text-sm text-text-dim py-6 justify-center">
      <div class="w-4 h-4 border-2 border-accent border-t-transparent rounded-full animate-spin"></div>
      Loading schema...
    </div>
  {:else}
    <!-- Error message + manual dataset entry -->
    {#if schemaError}
      <div class="mb-3 p-3 rounded-lg bg-warning/10 border border-warning/20 text-sm text-warning leading-relaxed">
        {schemaError}
      </div>
    {/if}

    <!-- Manual dataset entry (always shown when no databases found, or as additional input) -->
    {#if $schema.databases.length === 0 || schemaError}
      <div class="mb-3">
        <div class="flex gap-2">
          <input
            type="text"
            class="flex-1 px-3 py-2 rounded-lg bg-bg border border-border text-sm outline-none"
            placeholder="Enter dataset name..."
            bind:value={manualDataset}
            on:keydown={handleManualKeydown}
          />
          <button
            class="px-3 py-2 text-sm bg-accent text-white rounded-lg hover:bg-accent-hover disabled:opacity-50 font-medium"
            disabled={loadingManual || !manualDataset.trim()}
            on:click={loadManualDataset}
          >
            {#if loadingManual}
              <div class="w-4 h-4 border-2 border-white border-t-transparent rounded-full animate-spin"></div>
            {:else}
              Browse
            {/if}
          </button>
        </div>
      </div>
    {/if}

    <!-- Database selector -->
    {#if $schema.databases.length > 1}
      <select
        class="w-full px-3 py-2.5 mb-3 rounded-lg bg-bg border border-border text-sm outline-none"
        value={$schema.activeDatabase}
        on:change={(e) => selectDatabase(e.currentTarget.value)}
      >
        {#each $schema.databases as db}
          <option value={db}>{db}</option>
        {/each}
      </select>
    {:else if $schema.databases.length === 1}
      <div class="text-sm text-text-dim font-medium px-1 mb-3">{$schema.activeDatabase}</div>
    {/if}

    <!-- Loading tables indicator (dataset switch) -->
    {#if loadingTables}
      <div class="flex items-center gap-2 text-sm text-text-dim py-4 justify-center">
        <div class="w-4 h-4 border-2 border-accent border-t-transparent rounded-full animate-spin"></div>
        Loading tables...
      </div>
    {/if}

    <!-- Table list -->
    {#each $schema.tables as table (table.name)}
      <div class="mb-0.5">
        <button
          class="w-full flex items-center gap-2.5 px-3 py-2 rounded-lg text-left hover:bg-surface-hover group text-sm"
          on:click={() => expandTable(table)}
          on:dblclick={() => insertTableName(table.name)}
        >
          <span
            class="text-xs text-text-muted transition-transform {$schema.expandedTables.has(table.name)
              ? 'rotate-90'
              : ''}"
          >
            &#9654;
          </span>
          <span class="flex-1 truncate font-mono text-[13px]">{table.name}</span>
          <span class="text-xs text-text-muted opacity-0 group-hover:opacity-100">
            {table.type}
          </span>
        </button>

        {#if $schema.expandedTables.has(table.name)}
          <div class="ml-6 border-l border-border pl-3 mb-1">
            {#if table.row_count > 0 || table.size_bytes > 0}
              <div class="text-xs text-text-muted px-2 py-1">
                {#if table.row_count > 0}{formatNumber(table.row_count)} rows{/if}
                {#if table.size_bytes > 0} &middot; {formatBytes(table.size_bytes)}{/if}
              </div>
            {/if}

            {#if $schema.tableColumns[table.name]}
              {#each $schema.tableColumns[table.name] as col}
                <div
                  class="flex items-center gap-2 px-2 py-1 text-[13px] hover:bg-surface-hover rounded-md"
                >
                  <span class="font-mono truncate flex-1">{col.name}</span>
                  <span class="text-text-muted uppercase text-xs">{col.type}</span>
                  {#if col.is_primary}
                    <span class="text-warning text-xs font-medium">PK</span>
                  {/if}
                </div>
              {/each}
            {:else}
              <div class="flex items-center gap-2 text-xs text-text-muted px-2 py-1.5">
                {#if loadingColumn === table.name}
                  <div class="w-3 h-3 border-2 border-accent border-t-transparent rounded-full animate-spin"></div>
                {/if}
                Loading columns...
              </div>
            {/if}

            <button
              class="text-xs text-accent px-2 py-1 hover:underline font-medium flex items-center gap-1.5 disabled:opacity-50"
              disabled={previewingTable === table.name}
              on:click={() => handlePreview(table.name)}
            >
              {#if previewingTable === table.name}
                <div class="w-3 h-3 border-2 border-accent border-t-transparent rounded-full animate-spin"></div>
                Loading...
              {:else}
                Preview data
              {/if}
            </button>
          </div>
        {/if}
      </div>
    {/each}

    {#if $schema.tables.length === 0 && !$schema.loading && !schemaError && $schema.databases.length > 0}
      <div class="text-sm text-text-dim py-6 text-center">No tables found in this dataset</div>
    {/if}

    {#if $schema.tables.length === 0 && !$schema.loading && $schema.databases.length === 0 && !schemaError}
      <div class="text-sm text-text-dim py-6 text-center">
        Connect to a database to browse schema
      </div>
    {/if}
  {/if}
</div>
