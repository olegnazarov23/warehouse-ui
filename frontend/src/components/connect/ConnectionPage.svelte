<script lang="ts">
  import { onMount } from "svelte";
  import ConnectionForm from "./ConnectionForm.svelte";
  import ConnectionList from "./ConnectionList.svelte";
  import { savedConnections } from "../../lib/stores/connection";
  import { listSavedConnections } from "../../lib/api";
  import type { ConnectionConfig } from "../../lib/types";

  let showForm = false;
  let editConfig: ConnectionConfig | null = null;

  onMount(async () => {
    try {
      const conns = await listSavedConnections();
      savedConnections.set(conns ?? []);
    } catch {
      // store not ready
    }
  });

  function handleNew() {
    editConfig = null;
    showForm = true;
  }

  function handleEdit(e: CustomEvent<ConnectionConfig>) {
    editConfig = e.detail;
    showForm = true;
  }

  function handleBack() {
    showForm = false;
    editConfig = null;
  }
</script>

<div class="flex flex-col h-screen bg-bg">
  <!-- Drag region for window dragging on macOS -->
  <div class="wails-drag h-12 flex-shrink-0"></div>

  <div class="flex-1 overflow-y-auto px-6 pb-10">
    <div class="max-w-lg mx-auto">
      <div class="text-center mb-10">
        <div class="inline-flex items-center justify-center w-16 h-16 rounded-2xl bg-accent/10 mb-4">
          <svg width="28" height="28" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" class="text-accent">
            <path d="M4 7V4a2 2 0 0 1 2-2h12a2 2 0 0 1 2 2v3M4 7v13a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7M4 7h16M8 12h8M8 16h5" stroke-linecap="round"/>
          </svg>
        </div>
        <h1 class="text-2xl font-semibold tracking-tight">Warehouse UI</h1>
        <p class="text-text-dim text-[15px] mt-1">
          Connect to your database and start exploring
        </p>
      </div>

      {#if showForm}
        <ConnectionForm config={editConfig} on:back={handleBack} />
      {:else}
        <ConnectionList on:new={handleNew} on:edit={handleEdit} />
      {/if}
    </div>
  </div>
</div>
