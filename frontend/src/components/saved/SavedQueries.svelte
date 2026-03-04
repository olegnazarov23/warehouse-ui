<script lang="ts">
  import { onMount } from "svelte";
  import { listSavedQueries, deleteSavedQuery } from "../../lib/api";
  import { addTab } from "../../lib/stores/editor";
  import type { SavedQuery } from "../../lib/types";

  let queries: SavedQuery[] = [];
  let search = "";
  let loading = false;

  onMount(() => load());

  async function load() {
    loading = true;
    try {
      queries = (await listSavedQueries(search)) ?? [];
    } catch {
      queries = [];
    }
    loading = false;
  }

  async function handleDelete(id: string) {
    await deleteSavedQuery(id);
    queries = queries.filter((q) => q.id !== id);
  }

  function openInTab(q: SavedQuery) {
    addTab(q.name, q.sql);
  }

  let debounceTimer: ReturnType<typeof setTimeout>;
  function onSearch() {
    clearTimeout(debounceTimer);
    debounceTimer = setTimeout(load, 300);
  }
</script>

<div class="p-3 flex flex-col h-full">
  <input
    type="text"
    class="w-full px-3 py-2 mb-3 rounded-lg bg-bg border border-border text-sm outline-none"
    placeholder="Search saved queries..."
    bind:value={search}
    on:input={onSearch}
  />

  <div class="flex-1 overflow-auto space-y-1">
    {#if loading}
      <div class="text-sm text-text-dim text-center py-6">Loading...</div>
    {:else if queries.length === 0}
      <div class="text-sm text-text-dim text-center py-6">No saved queries</div>
    {:else}
      {#each queries as q (q.id)}
        <button
          class="w-full text-left p-3 rounded-lg hover:bg-surface-hover group"
          on:click={() => openInTab(q)}
        >
          <div class="text-sm font-medium">{q.name}</div>
          {#if q.description}
            <div class="text-xs text-text-dim mt-0.5 truncate">
              {q.description}
            </div>
          {/if}
          <div class="text-xs text-text-muted font-mono mt-1 truncate">
            {q.sql}
          </div>
          <span
            class="text-xs text-danger opacity-0 group-hover:opacity-100 hover:underline font-medium"
            on:click|stopPropagation={() => handleDelete(q.id)}
          >
            delete
          </span>
        </button>
      {/each}
    {/if}
  </div>
</div>
