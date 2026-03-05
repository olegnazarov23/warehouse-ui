<script lang="ts">
  import { onMount, onDestroy, tick } from "svelte";
  import { EventsOn, EventsOff } from "../../../wailsjs/runtime/runtime";
  import {
    activeTab,
    setTabResult,
    setTabDryRun,
    setTabExplain,
    setTabError,
    setTabRunning,
    updateTabSQL,
  } from "../../lib/stores/editor";
  import { currentDriverType } from "../../lib/stores/connection";

  // Running timer
  let runStartTime = 0;
  let runElapsed = "";
  let timerInterval: ReturnType<typeof setInterval> | null = null;

  function startTimer() {
    runStartTime = Date.now();
    runElapsed = "0.0s";
    timerInterval = setInterval(() => {
      const elapsed = (Date.now() - runStartTime) / 1000;
      runElapsed = elapsed < 60
        ? `${elapsed.toFixed(1)}s`
        : `${Math.floor(elapsed / 60)}m ${Math.floor(elapsed % 60)}s`;
    }, 100);
  }

  function stopTimer() {
    if (timerInterval) {
      clearInterval(timerInterval);
      timerInterval = null;
    }
    runElapsed = "";
  }

  // Track which tab is running so event handlers can update the right tab
  let runningTabId = "";

  // Keyboard shortcut handlers
  const onEditorRun = () => handleRun();
  const onEditorSave = () => openSaveDialog();
  const onEditorExplain = () => handleExplain();

  onMount(() => {
    // Wails events for async query results
    EventsOn("query:result", (data: any) => {
      if (runningTabId) {
        setTabResult(runningTabId, data);
        runningTabId = "";
      }
      localRunning = false;
      stopTimer();
    });
    EventsOn("query:error", (data: any) => {
      if (runningTabId) {
        setTabError(runningTabId, data?.error || "Query failed");
        setTabRunning(runningTabId, false);
        runningTabId = "";
      }
      localRunning = false;
      stopTimer();
    });

    // Keyboard shortcuts from Monaco editor
    document.addEventListener("editor:run", onEditorRun);
    document.addEventListener("editor:save", onEditorSave);
    document.addEventListener("editor:explain", onEditorExplain);
  });

  onDestroy(() => {
    if (timerInterval) clearInterval(timerInterval);
    EventsOff("query:result");
    EventsOff("query:error");
    document.removeEventListener("editor:run", onEditorRun);
    document.removeEventListener("editor:save", onEditorSave);
    document.removeEventListener("editor:explain", onEditorExplain);
  });
  import {
    execute,
    executeAsync,
    dryRun,
    cancelQuery,
    exportCSV,
    exportJSON,
    exportExcel,
    optimizeQuery,
    explainQuery,
    saveQuery,
    aiGenerateSQL,
  } from "../../lib/api";
  import { formatBytes, formatCost } from "../../lib/format";
  import type { OptimizeResult } from "../../lib/types";

  // Ask AI — natural language to SQL
  let aiPrompt = "";
  let aiGenerating = false;

  async function handleAiGenerate() {
    if (!aiPrompt.trim() || aiGenerating) return;
    aiGenerating = true;
    try {
      const sql = await aiGenerateSQL(aiPrompt.trim());
      if (sql) {
        const tab = $activeTab;
        if (tab) updateTabSQL(tab.id, sql);
      }
      aiPrompt = "";
    } catch (e: any) {
      console.error("AI generate failed:", e);
    }
    aiGenerating = false;
  }

  function handleAiKeydown(e: KeyboardEvent) {
    if (e.key === "Enter") { e.preventDefault(); handleAiGenerate(); }
    if (e.key === "Escape") { aiPrompt = ""; (e.target as HTMLInputElement)?.blur(); }
  }

  function handleRun() {
    const tab = $activeTab;
    if (!tab || !tab.sql.trim()) return;

    localRunning = true;
    runningTabId = tab.id;
    setTabRunning(tab.id, true);
    setTabError(tab.id, "");
    startTimer();

    // Fire-and-forget: result comes back via "query:result" / "query:error" events
    executeAsync(tab.sql, 10000);
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
      localRunning = false;
      stopTimer();
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

  let explaining = false;

  async function handleExplain() {
    const tab = $activeTab;
    if (!tab || !tab.sql.trim()) return;
    explaining = true;
    try {
      const result = await explainQuery(tab.sql);
      setTabExplain(tab.id, result);
    } catch (e: any) {
      setTabError(tab.id, `Explain failed: ${e?.message || e}`);
    }
    explaining = false;
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

  let showSaveDialog = false;
  let saveName = "";
  let saving = false;

  function openSaveDialog() {
    const tab = $activeTab;
    if (!tab?.sql?.trim()) return;
    saveName = tab.title?.startsWith("Query") ? "" : tab.title || "";
    showSaveDialog = true;
    // Focus input after render
    setTimeout(() => {
      const el = document.getElementById("save-query-name");
      if (el) el.focus();
    }, 50);
  }

  async function handleSave() {
    const tab = $activeTab;
    if (!tab?.sql?.trim() || !saveName.trim()) return;
    saving = true;
    try {
      await saveQuery(saveName.trim(), tab.sql, "", "[]");
      showSaveDialog = false;
      saveName = "";
    } catch (e: any) {
      console.error("Save failed:", e);
    }
    saving = false;
  }

  function handleSaveKeydown(e: KeyboardEvent) {
    if (e.key === "Enter") handleSave();
    if (e.key === "Escape") showSaveDialog = false;
  }

  // Local running flag — updates instantly without waiting for derived store
  let localRunning = false;

  $: supportsCost =
    $currentDriverType === "bigquery" || $currentDriverType === "clickhouse";
</script>

<div class="flex flex-col flex-shrink-0">
  <div
    class="flex items-center gap-2 px-4 py-2 border-b border-border bg-surface flex-shrink-0"
  >
    <!-- Run -->
    {#if localRunning}
      <button
        class="px-4 py-2 text-sm font-semibold bg-danger text-white rounded-lg hover:bg-danger/80 flex items-center gap-2"
        on:click={handleCancel}
      >
        <div class="w-3 h-3 border-2 border-white border-t-transparent rounded-full animate-spin"></div>
        Cancel
      </button>
      {#if runElapsed}
        <span class="text-sm text-text-dim font-mono tabular-nums">{runElapsed}</span>
      {/if}
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

    <!-- Explain (SQL only) -->
    {#if $currentDriverType !== "mongodb"}
      <button
        class="px-3 py-2 text-sm text-text-dim rounded-lg hover:bg-surface-hover hover:text-text font-medium disabled:opacity-50"
        disabled={explaining || !$activeTab?.sql?.trim()}
        on:click={handleExplain}
      >
        {#if explaining}
          <span class="flex items-center gap-1.5">
            <span class="w-3 h-3 border-2 border-current border-t-transparent rounded-full animate-spin"></span>
            Explain
          </span>
        {:else}
          Explain
        {/if}
      </button>

      <!-- Format -->
      <button
        class="px-3 py-2 text-sm text-text-dim rounded-lg hover:bg-surface-hover hover:text-text font-medium"
        on:click={handleFormat}
      >
        Format
      </button>
    {/if}

    <!-- Save -->
    <button
      class="px-3 py-2 text-sm text-text-dim rounded-lg hover:bg-surface-hover hover:text-text font-medium disabled:opacity-50"
      on:click={openSaveDialog}
      disabled={!$activeTab?.sql?.trim()}
      title="Save this query (Cmd+S)"
    >
      Save
    </button>

    <!-- Ask AI — natural language to SQL -->
    <div class="flex items-center gap-1.5 ml-2">
      <input
        type="text"
        class="w-48 px-3 py-1.5 text-xs rounded-lg bg-bg border border-border outline-none focus:border-accent focus:w-72 transition-all placeholder:text-text-muted"
        placeholder="Ask AI to write SQL..."
        bind:value={aiPrompt}
        on:keydown={handleAiKeydown}
        disabled={aiGenerating}
      />
      {#if aiGenerating}
        <div class="w-3 h-3 border-2 border-accent border-t-transparent rounded-full animate-spin"></div>
      {/if}
    </div>

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

  <!-- Running progress bar -->
  {#if localRunning}
    <div class="h-0.5 bg-bg overflow-hidden">
      <div class="h-full bg-accent animate-progress-bar"></div>
    </div>
  {/if}

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

  <!-- Save query dialog -->
  {#if showSaveDialog}
    <div class="px-4 py-2 border-b border-border bg-surface flex items-center gap-2">
      <input
        id="save-query-name"
        type="text"
        class="flex-1 px-3 py-1.5 text-sm rounded-lg bg-bg border border-border outline-none focus:border-accent"
        placeholder="Query name..."
        bind:value={saveName}
        on:keydown={handleSaveKeydown}
      />
      <button
        class="px-3 py-1.5 text-sm font-medium bg-accent text-white rounded-lg hover:bg-accent-hover disabled:opacity-50"
        on:click={handleSave}
        disabled={saving || !saveName.trim()}
      >
        {saving ? "Saving..." : "Save"}
      </button>
      <button
        class="px-2 py-1.5 text-sm text-text-muted hover:text-text"
        on:click={() => (showSaveDialog = false)}
      >
        Cancel
      </button>
    </div>
  {/if}
</div>
