<script lang="ts">
  import {
    activeTab,
    setTabResult,
    setTabDryRun,
    setTabError,
    setTabRunning,
    updateTabSQL,
  } from "../../lib/stores/editor";
  import { currentDriverType } from "../../lib/stores/connection";
  import {
    execute,
    dryRun,
    cancelQuery,
    exportCSV,
    exportJSON,
    exportExcel,
    optimizeQuery,
  } from "../../lib/api";
  import { formatBytes, formatCost } from "../../lib/format";
  import type { OptimizeResult } from "../../lib/types";

  async function handleRun() {
    const tab = $activeTab;
    if (!tab || !tab.sql.trim()) return;

    setTabRunning(tab.id, true);
    setTabError(tab.id, "");

    try {
      const result = await execute(tab.sql, 10000);
      setTabResult(tab.id, result);
    } catch (e: any) {
      const msg = typeof e === "string" ? e : e?.message || "Query failed";
      setTabError(tab.id, msg);
    }
  }

  let dryRunning = false;
  let exporting = "";
  let optimizing = false;
  let optimizeResult: OptimizeResult | null = null;

  async function handleDryRun() {
    const tab = $activeTab;
    if (!tab || !tab.sql.trim()) return;
    dryRunning = true;

    try {
      const result = await dryRun(tab.sql);
      setTabDryRun(tab.id, result);
    } catch (e: any) {
      const msg = typeof e === "string" ? e : e?.message || "Dry run failed";
      setTabError(tab.id, msg);
    }
    dryRunning = false;
  }

  async function handleCancel() {
    try {
      await cancelQuery();
      const tab = $activeTab;
      if (tab) setTabRunning(tab.id, false);
    } catch (e) {
      console.error("Cancel failed:", e);
    }
  }

  async function handleOptimize() {
    const tab = $activeTab;
    if (!tab || !tab.sql.trim()) return;
    optimizing = true;
    optimizeResult = null;

    try {
      const result = await optimizeQuery(tab.sql);
      optimizeResult = result;

      // If an optimization was found, update the editor
      if (result.optimized_sql && result.optimized_sql !== result.original_sql) {
        updateTabSQL(tab.id, result.optimized_sql);
        // Also update dry run if we got new metrics
        if (result.optimized_dry_run) {
          setTabDryRun(tab.id, result.optimized_dry_run);
        }
      }
    } catch (e: any) {
      const msg = typeof e === "string" ? e : e?.message || "Optimization failed";
      setTabError(tab.id, msg);
    }
    optimizing = false;
  }

  function dismissOptimizeResult() {
    optimizeResult = null;
  }

  async function handleExportCSV() {
    const tab = $activeTab;
    if (!tab?.sql.trim()) return;
    exporting = "csv";
    try {
      await exportCSV(tab.sql);
    } catch (e: any) {
      console.error("Export failed:", e);
    }
    exporting = "";
  }

  async function handleExportJSON() {
    const tab = $activeTab;
    if (!tab?.sql.trim()) return;
    exporting = "json";
    try {
      await exportJSON(tab.sql);
    } catch (e: any) {
      console.error("Export failed:", e);
    }
    exporting = "";
  }

  async function handleExportExcel() {
    const tab = $activeTab;
    if (!tab?.sql.trim()) return;
    exporting = "excel";
    try {
      await exportExcel(tab.sql);
    } catch (e: any) {
      console.error("Export failed:", e);
    }
    exporting = "";
  }

  function handleFormat() {
    import("sql-formatter").then(({ format }) => {
      const tab = $activeTab;
      if (!tab) return;
      try {
        const formatted = format(tab.sql, { language: "sql" });
        updateTabSQL(tab.id, formatted);
      } catch {
        // formatting failed
      }
    });
  }

  $: supportsCost =
    $currentDriverType === "bigquery" || $currentDriverType === "clickhouse";
</script>

<div class="flex flex-col flex-shrink-0">
  <div
    class="flex items-center gap-2 px-4 py-2 border-b border-border bg-surface flex-shrink-0"
  >
    <!-- Run -->
    {#if $activeTab?.running}
      <button
        class="px-4 py-2 text-sm font-semibold bg-danger text-white rounded-lg hover:bg-danger/80"
        on:click={handleCancel}
      >
        Cancel
      </button>
    {:else}
      <button
        class="px-4 py-2 text-sm font-semibold bg-accent text-white rounded-lg hover:bg-accent-hover shadow-sm shadow-accent/10"
        on:click={handleRun}
      >
        Run &#9654;
      </button>
    {/if}

    <!-- Dry Run -->
    {#if supportsCost}
      <button
        class="px-3 py-2 text-sm text-text-dim rounded-lg hover:bg-surface-hover hover:text-text font-medium disabled:opacity-50 flex items-center gap-1.5"
        on:click={handleDryRun}
        disabled={dryRunning}
        title="Estimate cost before running"
      >
        {#if dryRunning}
          <div class="w-3 h-3 border-2 border-accent border-t-transparent rounded-full animate-spin"></div>
        {/if}
        Dry Run
      </button>
    {/if}

    <!-- Optimize with AI -->
    <button
      class="px-3 py-2 text-sm rounded-lg font-medium disabled:opacity-50 flex items-center gap-1.5 {optimizeResult && optimizeResult.optimized_sql !== optimizeResult.original_sql
        ? 'bg-success/10 text-success hover:bg-success/20'
        : 'text-accent bg-accent/10 hover:bg-accent/20'}"
      on:click={handleOptimize}
      disabled={optimizing || !$activeTab?.sql?.trim()}
      title="Use AI to optimize this query for better performance"
    >
      {#if optimizing}
        <div class="w-3 h-3 border-2 border-accent border-t-transparent rounded-full animate-spin"></div>
        Optimizing...
      {:else}
        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <path d="M12 2a2 2 0 0 1 2 2c0 .74-.4 1.39-1 1.73V7h1a7 7 0 0 1 7 7h1a1 1 0 0 1 1 1v3a1 1 0 0 1-1 1h-1.27A7 7 0 0 1 7.27 19H6a1 1 0 0 1-1-1v-3a1 1 0 0 1 1-1h1a7 7 0 0 1 7-7h1V5.73c-.6-.34-1-.99-1-1.73a2 2 0 0 1 2-2z"/>
          <path d="M10 17v-6"/><path d="M14 17v-6"/>
        </svg>
        Optimize
      {/if}
    </button>

    <!-- Format -->
    <button
      class="px-3 py-2 text-sm text-text-dim rounded-lg hover:bg-surface-hover hover:text-text font-medium"
      on:click={handleFormat}
    >
      Format
    </button>

    <div class="flex-1"></div>

    <!-- Dry run results -->
    {#if $activeTab?.dryRun}
      <div class="flex items-center gap-2 text-sm">
        {#if $activeTab.dryRun.valid}
          {#if $activeTab.dryRun.statement_type}
            <span class="px-2 py-1 rounded-lg text-xs font-semibold bg-accent/10 text-accent">
              {$activeTab.dryRun.statement_type}
            </span>
          {/if}
          {#if $activeTab.dryRun.estimated_rows > 0}
            <span class="text-text-dim" title="Estimated rows">
              ~{$activeTab.dryRun.estimated_rows.toLocaleString()} rows
            </span>
          {/if}
          {#if $activeTab.dryRun.estimated_bytes > 0}
            <span class="text-text-dim">
              {formatBytes($activeTab.dryRun.estimated_bytes)}
            </span>
          {/if}
          {#if $activeTab.dryRun.estimated_cost_usd > 0}
            <span
              class="px-2 py-1 rounded-lg text-xs font-semibold {$activeTab.dryRun
                .estimated_cost_usd > 1
                ? 'bg-warning/15 text-warning'
                : 'bg-success/15 text-success'}"
            >
              {formatCost($activeTab.dryRun.estimated_cost_usd)}
            </span>
          {/if}
          {#if $activeTab.dryRun.referenced_tables && $activeTab.dryRun.referenced_tables.length > 0}
            <span class="text-text-muted text-xs" title={$activeTab.dryRun.referenced_tables.join(', ')}>
              {$activeTab.dryRun.referenced_tables.length} table{$activeTab.dryRun.referenced_tables.length > 1 ? 's' : ''}
            </span>
          {/if}
          {#if !$activeTab.dryRun.estimated_bytes && !$activeTab.dryRun.estimated_rows}
            <span class="px-2 py-1 rounded-lg text-xs font-semibold bg-success/15 text-success">
              Valid
            </span>
          {/if}
        {:else}
          <span class="text-danger text-sm">{$activeTab.dryRun.error}</span>
        {/if}
      </div>
    {/if}

    <!-- Export -->
    <div class="flex items-center gap-1">
      <button
        class="px-3 py-2 text-sm text-text-dim rounded-lg hover:bg-surface-hover hover:text-text font-medium disabled:opacity-50 flex items-center gap-1.5"
        disabled={!!exporting}
        on:click={handleExportCSV}
      >
        {#if exporting === "csv"}
          <div class="w-3 h-3 border-2 border-accent border-t-transparent rounded-full animate-spin"></div>
        {/if}
        CSV
      </button>
      <button
        class="px-3 py-2 text-sm text-text-dim rounded-lg hover:bg-surface-hover hover:text-text font-medium disabled:opacity-50 flex items-center gap-1.5"
        disabled={!!exporting}
        on:click={handleExportJSON}
      >
        {#if exporting === "json"}
          <div class="w-3 h-3 border-2 border-accent border-t-transparent rounded-full animate-spin"></div>
        {/if}
        JSON
      </button>
      <button
        class="px-3 py-2 text-sm text-text-dim rounded-lg hover:bg-surface-hover hover:text-text font-medium disabled:opacity-50 flex items-center gap-1.5"
        disabled={!!exporting}
        on:click={handleExportExcel}
      >
        {#if exporting === "excel"}
          <div class="w-3 h-3 border-2 border-accent border-t-transparent rounded-full animate-spin"></div>
        {/if}
        Excel
      </button>
    </div>
  </div>

  <!-- Optimize result banner -->
  {#if optimizeResult}
    <div class="px-4 py-2 border-b border-border bg-surface flex items-center gap-3 text-sm">
      {#if optimizeResult.optimized_sql !== optimizeResult.original_sql}
        <span class="text-success font-medium">Optimized</span>
        <span class="text-text-dim">
          {optimizeResult.iterations} iteration{optimizeResult.iterations > 1 ? 's' : ''}
        </span>
        {#if optimizeResult.original_dry_run?.estimated_bytes && optimizeResult.optimized_dry_run?.estimated_bytes}
          <span class="text-text-dim">
            {formatBytes(optimizeResult.original_dry_run.estimated_bytes)} &rarr; {formatBytes(optimizeResult.optimized_dry_run.estimated_bytes)}
          </span>
        {/if}
        {#if optimizeResult.improvements.length > 0}
          <span class="text-text-muted text-xs truncate flex-1" title={optimizeResult.improvements.join('; ')}>
            {optimizeResult.improvements.join('; ')}
          </span>
        {/if}
      {:else}
        <span class="text-text-dim">{optimizeResult.explanation}</span>
      {/if}
      <button
        class="text-text-muted hover:text-text text-xs ml-auto"
        on:click={dismissOptimizeResult}
      >
        Dismiss
      </button>
    </div>
  {/if}
</div>
