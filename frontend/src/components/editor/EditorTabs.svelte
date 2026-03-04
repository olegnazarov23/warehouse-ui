<script lang="ts">
  import {
    editor,
    addTab,
    closeTab,
    setActiveTab,
  } from "../../lib/stores/editor";
</script>

<div class="flex items-center bg-surface border-b border-border h-10 flex-shrink-0 overflow-x-auto">
  {#each $editor.tabs as tab (tab.id)}
    <button
      class="flex items-center gap-1.5 px-4 h-full text-sm border-r border-border whitespace-nowrap transition-colors
        {$editor.activeTabId === tab.id
        ? 'bg-bg text-text font-medium'
        : 'text-text-dim hover:text-text hover:bg-surface-hover'}"
      on:click={() => setActiveTab(tab.id)}
    >
      <span class="truncate max-w-36">{tab.title}</span>
      {#if tab.dirty}
        <span class="w-2 h-2 rounded-full bg-accent"></span>
      {/if}
      {#if tab.running}
        <span class="w-2 h-2 rounded-full bg-warning animate-pulse"></span>
      {/if}
      {#if $editor.tabs.length > 1}
        <button
          class="ml-1 text-text-muted hover:text-danger text-xs leading-none"
          on:click|stopPropagation={() => closeTab(tab.id)}
        >
          &times;
        </button>
      {/if}
    </button>
  {/each}

  <button
    class="px-3 h-full text-text-muted hover:text-text text-[15px]"
    on:click={() => addTab()}
    title="New tab"
  >
    +
  </button>
</div>
