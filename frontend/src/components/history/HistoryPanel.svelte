<script lang="ts">
  import { onMount } from "svelte";
  import { getHistory, deleteHistory, clearHistory } from "../../lib/api";
  import { addTab } from "../../lib/stores/editor";
  import { formatDuration, formatCost, truncate } from "../../lib/format";
  import type { HistoryEntry } from "../../lib/types";

  let entries: HistoryEntry[] = [];
  let search = "";
  let loading = false;

  onMount(() => load());

  async function load() {
    loading = true;
    try {
      entries = (await getHistory(search, 100, 0)) ?? [];
    } catch {
      entries = [];
    }
    loading = false;
  }

  async function handleClear() {
    await clearHistory();
    entries = [];
  }

  async function handleDelete(id: string) {
    await deleteHistory(id);
    entries = entries.filter((e) => e.id !== id);
  }

  function openInTab(entry: HistoryEntry) {
    addTab(truncate(entry.sql, 30), entry.sql);
  }

  let debounceTimer: ReturnType<typeof setTimeout>;
  function onSearch() {
    clearTimeout(debounceTimer);
    debounceTimer = setTimeout(load, 300);
  }
</script>

<div class="p-3 flex flex-col h-full">
  <!-- Search -->
  <div class="flex gap-2 mb-3">
    <input
      type="text"
      class="flex-1 px-3 py-2 rounded-lg bg-bg border border-border text-sm outline-none"
      placeholder="Search history..."
      bind:value={search}
      on:input={onSearch}
    />
    {#if entries.length > 0}
      <button
        class="text-xs text-text-muted hover:text-danger font-medium px-2"
        on:click={handleClear}
      >
        Clear
      </button>
    {/if}
  </div>

  <!-- List -->
  <div class="flex-1 overflow-auto space-y-1">
    {#if loading}
      <div class="text-sm text-text-dim text-center py-6">Loading...</div>
    {:else if entries.length === 0}
      <div class="text-sm text-text-dim text-center py-6">No history yet</div>
    {:else}
      {#each entries as entry (entry.id)}
        <button
          class="w-full text-left p-3 rounded-lg hover:bg-surface-hover group"
          on:click={() => openInTab(entry)}
        >
          <div class="text-[13px] font-mono truncate">{entry.sql}</div>
          <div class="flex items-center gap-2 mt-1 text-xs text-text-muted">
            <span
              class="w-2 h-2 rounded-full {entry.status === 'completed'
                ? 'bg-success'
                : 'bg-danger'}"
            ></span>
            {#if entry.duration_ms}
              <span>{formatDuration(entry.duration_ms)}</span>
            {/if}
            {#if entry.cost_usd}
              <span>{formatCost(entry.cost_usd)}</span>
            {/if}
            {#if entry.row_count}
              <span>{entry.row_count} rows</span>
            {/if}
            <span class="ml-auto opacity-0 group-hover:opacity-100 text-danger hover:underline font-medium"
              on:click|stopPropagation={() => handleDelete(entry.id)}
            >
              delete
            </span>
          </div>
        </button>
      {/each}
    {/if}
  </div>
</div>
