<script lang="ts">
  import {
    editor,
    addTab,
    closeTab,
    setActiveTab,
    renameTab,
  } from "../../lib/stores/editor";

  let editingTabId = "";
  let editValue = "";

  function startRename(tabId: string, currentTitle: string) {
    editingTabId = tabId;
    editValue = currentTitle;
    setTimeout(() => {
      const el = document.getElementById(`tab-rename-${tabId}`);
      if (el) { (el as HTMLInputElement).focus(); (el as HTMLInputElement).select(); }
    }, 10);
  }

  function commitRename() {
    if (editingTabId && editValue.trim()) {
      renameTab(editingTabId, editValue.trim());
    }
    editingTabId = "";
  }

  function handleRenameKeydown(e: KeyboardEvent) {
    if (e.key === "Enter") commitRename();
    if (e.key === "Escape") { editingTabId = ""; }
  }
</script>

<div class="flex items-center bg-surface border-b border-border h-10 flex-shrink-0 overflow-x-auto">
  {#each $editor.tabs as tab (tab.id)}
    <button
      class="flex items-center gap-1.5 px-4 h-full text-sm border-r border-border whitespace-nowrap transition-colors
        {$editor.activeTabId === tab.id
        ? 'bg-bg text-text font-medium'
        : 'text-text-dim hover:text-text hover:bg-surface-hover'}"
      on:click={() => setActiveTab(tab.id)}
      on:dblclick|stopPropagation={() => startRename(tab.id, tab.title)}
    >
      {#if editingTabId === tab.id}
        <!-- svelte-ignore a11y_autofocus -->
        <input
          id="tab-rename-{tab.id}"
          type="text"
          class="bg-transparent border-b border-accent text-sm outline-none w-24 text-text"
          bind:value={editValue}
          on:blur={commitRename}
          on:keydown={handleRenameKeydown}
          on:click|stopPropagation
        />
      {:else}
        <span class="truncate max-w-36">{tab.title}</span>
      {/if}
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
