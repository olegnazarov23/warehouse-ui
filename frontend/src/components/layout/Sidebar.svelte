<script lang="ts">
  import SchemaTree from "../schema/SchemaTree.svelte";
  import HistoryPanel from "../history/HistoryPanel.svelte";
  import SavedQueries from "../saved/SavedQueries.svelte";
  import TemplatesPanel from "../saved/TemplatesPanel.svelte";
  import type { SidebarTab } from "../../lib/types";

  let activeTab: SidebarTab = "schema";

  const tabs: { id: SidebarTab; label: string }[] = [
    { id: "schema", label: "Schema" },
    { id: "saved", label: "Saved" },
    { id: "history", label: "History" },
    { id: "templates", label: "Guide" },
  ];
</script>

<div class="flex flex-col h-full bg-surface">
  <!-- Tab bar (macOS segmented control style) -->
  <div class="flex p-2 gap-0.5 bg-surface">
    {#each tabs as tab}
      <button
        class="flex-1 py-2 text-sm text-center rounded-lg font-medium transition-all
          {activeTab === tab.id
          ? 'bg-surface-hover text-text shadow-sm'
          : 'text-text-dim hover:text-text'}"
        on:click={() => (activeTab = tab.id)}
      >
        {tab.label}
      </button>
    {/each}
  </div>

  <!-- Content -->
  <div class="flex-1 overflow-auto border-t border-border">
    {#if activeTab === "schema"}
      <SchemaTree />
    {:else if activeTab === "saved"}
      <SavedQueries />
    {:else if activeTab === "history"}
      <HistoryPanel />
    {:else if activeTab === "templates"}
      <TemplatesPanel />
    {/if}
  </div>
</div>
