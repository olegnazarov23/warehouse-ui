<script lang="ts">
  import type { EditorTab } from "../../lib/types";
  import { formatBytes, formatCost, formatDuration, formatNumber } from "../../lib/format";
  import { currentDriverType } from "../../lib/stores/connection";
  import { analyzeSql } from "../../lib/sql-hints";
  import DataGrid from "./DataGrid.svelte";

  export let tab: EditorTab | undefined;

  let activePane: "data" | "messages" = "data";

  $: sqlHints = tab?.sql ? analyzeSql(tab.sql, $currentDriverType) : [];
</script>

<div class="flex flex-col h-full bg-bg">
  <!-- Tab bar -->
  <div class="flex items-center border-b border-border px-4 h-9 flex-shrink-0 gap-4">
    <button
      class="text-sm font-medium {activePane === 'data' ? 'text-text' : 'text-text-dim hover:text-text'}"
      on:click={() => (activePane = "data")}
    >
      Results
    </button>
    <button
      class="text-sm font-medium flex items-center gap-1.5 {activePane === 'messages'
        ? 'text-text'
        : 'text-text-dim hover:text-text'}"
      on:click={() => (activePane = "messages")}
    >
      Messages
      {#if sqlHints.length > 0 || (tab?.dryRun?.warnings?.length ?? 0) > 0}
        <span class="px-1.5 py-0.5 rounded-full text-[10px] font-semibold bg-warning/15 text-warning">
          {sqlHints.length + (tab?.dryRun?.warnings?.length ?? 0)}
        </span>
      {/if}
    </button>

    <div class="flex-1"></div>

    <!-- Stats bar -->
    {#if tab?.result}
      <div class="flex items-center gap-3 text-xs text-text-dim">
        <span>{formatNumber(tab.result.row_count)} rows</span>
        {#if tab.result.duration_ms}
          <span>{formatDuration(tab.result.duration_ms)}</span>
        {/if}
        {#if tab.result.bytes_processed != null}
          <span>{formatBytes(tab.result.bytes_processed)}</span>
        {/if}
        {#if tab.result.cost_usd != null}
          <span
            class="px-1.5 py-0.5 rounded-md text-xs font-medium {tab.result.cost_usd > 1
              ? 'bg-warning/15 text-warning'
              : 'bg-success/15 text-success'}"
          >
            {formatCost(tab.result.cost_usd)}
          </span>
        {/if}
        {#if tab.result.cache_hit}
          <span class="text-success font-medium">cached</span>
        {/if}
      </div>
    {/if}
  </div>

  <!-- Content -->
  <div class="flex-1 overflow-auto">
    {#if activePane === "data"}
      {#if tab?.running}
        <div class="flex items-center justify-center h-full gap-3">
          <div class="w-5 h-5 border-2 border-accent border-t-transparent rounded-full animate-spin"></div>
          <div class="text-text-dim text-sm">Running query...</div>
        </div>
      {:else if tab?.error}
        <div class="p-5">
          <div class="p-4 rounded-xl bg-danger/10 border border-danger/20 text-danger text-sm font-mono whitespace-pre-wrap">
            {tab.error}
          </div>
        </div>
      {:else if tab?.result}
        <DataGrid
          columns={tab.result.columns}
          columnTypes={tab.result.column_types}
          rows={tab.result.rows}
        />
      {:else}
        <div class="flex items-center justify-center h-full text-text-muted text-sm">
          Run a query to see results
        </div>
      {/if}
    {:else}
      <!-- Messages / Warnings / Hints -->
      <div class="p-5 space-y-3 text-sm">
        <!-- Query timing breakdown -->
        {#if tab?.result}
          <div class="p-3 rounded-xl bg-surface border border-border">
            <div class="text-xs text-text-muted font-medium mb-2">Query Performance</div>
            <div class="grid grid-cols-2 sm:grid-cols-4 gap-3">
              <div>
                <div class="text-lg font-semibold text-text">{formatDuration(tab.result.duration_ms)}</div>
                <div class="text-xs text-text-muted">Duration</div>
              </div>
              <div>
                <div class="text-lg font-semibold text-text">{formatNumber(tab.result.row_count)}</div>
                <div class="text-xs text-text-muted">Rows returned</div>
              </div>
              {#if tab.result.bytes_processed != null}
                <div>
                  <div class="text-lg font-semibold text-text">{formatBytes(tab.result.bytes_processed)}</div>
                  <div class="text-xs text-text-muted">Data scanned</div>
                </div>
              {/if}
              {#if tab.result.cost_usd != null}
                <div>
                  <div class="text-lg font-semibold {tab.result.cost_usd > 1 ? 'text-warning' : 'text-success'}">
                    {formatCost(tab.result.cost_usd)}
                  </div>
                  <div class="text-xs text-text-muted">Cost{tab.result.cache_hit ? ' (cached)' : ''}</div>
                </div>
              {/if}
            </div>
            {#if tab.result.total_rows > tab.result.row_count}
              <div class="mt-2 text-xs text-text-muted">
                Showing {formatNumber(tab.result.row_count)} of {formatNumber(tab.result.total_rows)} total rows (limited)
              </div>
            {/if}
          </div>
        {/if}

        <!-- Dry run warnings -->
        {#if tab?.dryRun?.warnings?.length}
          {#each tab.dryRun.warnings as warn}
            <div class="p-3 rounded-xl bg-warning/10 border border-warning/20 text-warning text-sm">
              {warn}
            </div>
          {/each}
        {/if}

        <!-- SQL improvement suggestions -->
        {#if sqlHints.length > 0}
          <div>
            <div class="text-xs text-text-muted font-medium mb-2">Query Suggestions</div>
            <div class="space-y-2">
              {#each sqlHints as hint}
                <div class="p-3 rounded-xl border text-sm {
                  hint.level === 'warning' ? 'bg-warning/5 border-warning/20' :
                  hint.level === 'perf' ? 'bg-accent/5 border-accent/20' :
                  'bg-surface border-border'
                }">
                  <div class="flex items-center gap-2 mb-1">
                    {#if hint.level === 'warning'}
                      <span class="text-warning font-medium">Warning</span>
                    {:else if hint.level === 'perf'}
                      <span class="text-accent font-medium">Performance</span>
                    {:else}
                      <span class="text-text-dim font-medium">Tip</span>
                    {/if}
                    <span class="text-text-dim">{hint.message}</span>
                  </div>
                  <div class="text-text-muted text-xs">{hint.suggestion}</div>
                </div>
              {/each}
            </div>
          </div>
        {/if}

        <!-- Referenced tables -->
        {#if tab?.dryRun?.referenced_tables?.length}
          <div>
            <div class="text-xs text-text-muted font-medium mb-2">Referenced Tables</div>
            <div class="flex flex-wrap gap-1.5">
              {#each tab.dryRun.referenced_tables as table}
                <span class="px-2 py-1 rounded-lg bg-surface border border-border text-xs text-text-dim font-mono">
                  {table}
                </span>
              {/each}
            </div>
          </div>
        {/if}

        <!-- Empty state -->
        {#if !tab?.result && !tab?.dryRun?.warnings?.length && sqlHints.length === 0 && !tab?.error}
          <div class="text-text-muted text-sm">No messages</div>
        {/if}
      </div>
    {/if}
  </div>
</div>
