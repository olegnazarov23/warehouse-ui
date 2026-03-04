<script lang="ts">
  import { onMount } from "svelte";
  import { listTemplates } from "../../lib/api";
  import { addTab } from "../../lib/stores/editor";
  import { currentDriverType } from "../../lib/stores/connection";
  import type { QueryTemplate } from "../../lib/types";

  let templates: QueryTemplate[] = [];
  let loading = false;

  onMount(() => load());

  // Reload when driver changes
  $: if ($currentDriverType) load();

  async function load() {
    loading = true;
    try {
      templates = (await listTemplates($currentDriverType ?? "")) ?? [];
    } catch {
      templates = [];
    }
    loading = false;
  }

  function useTemplate(t: QueryTemplate) {
    addTab(t.name, t.sql);
  }

  // Group by category
  $: grouped = templates.reduce(
    (acc, t) => {
      const cat = t.category || "General";
      if (!acc[cat]) acc[cat] = [];
      acc[cat].push(t);
      return acc;
    },
    {} as Record<string, QueryTemplate[]>
  );
</script>

<div class="p-2 flex flex-col h-full overflow-auto">
  <div class="text-xs text-text-dim mb-2 px-1">
    Built-in query templates for {$currentDriverType || "your database"}
  </div>

  {#if loading}
    <div class="text-xs text-text-dim text-center py-4">Loading...</div>
  {:else if templates.length === 0}
    <div class="text-xs text-text-dim text-center py-4">
      No templates for this driver
    </div>
  {:else}
    {#each Object.entries(grouped) as [category, items]}
      <div class="mb-3">
        <div class="text-[10px] text-text-muted uppercase tracking-wider px-1 mb-1">
          {category}
        </div>
        {#each items as t (t.id)}
          <button
            class="w-full text-left p-2 rounded hover:bg-surface-hover"
            on:click={() => useTemplate(t)}
          >
            <div class="text-xs font-medium">{t.name}</div>
            <div class="text-[10px] text-text-dim mt-0.5">{t.description}</div>
          </button>
        {/each}
      </div>
    {/each}
  {/if}
</div>
