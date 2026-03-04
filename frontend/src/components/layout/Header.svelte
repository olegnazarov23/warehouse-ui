<script lang="ts">
  import { connectionStatus } from "../../lib/stores/connection";
  import { toggleAiPanel, ai } from "../../lib/stores/ai";
  import { disconnect } from "../../lib/api";

  async function handleDisconnect() {
    try {
      await disconnect();
      connectionStatus.set({
        connected: false,
        id: "",
        name: "",
        driver_type: "postgres",
        database: "",
      });
    } catch (e: any) {
      console.error("Disconnect failed:", e);
    }
  }
</script>

<header
  class="wails-drag flex items-center justify-between h-12 px-5 border-b border-border bg-surface/80 backdrop-blur-sm flex-shrink-0"
>
  <!-- Left: Logo + connection info -->
  <div class="flex items-center gap-3">
    <span class="font-semibold text-[15px] tracking-tight">Warehouse</span>

    {#if $connectionStatus.connected}
      <div class="flex items-center gap-2 text-sm text-text-dim">
        <span class="w-2 h-2 rounded-full bg-success"></span>
        <span class="font-medium">{$connectionStatus.name || $connectionStatus.driver_type}</span>
        {#if $connectionStatus.database}
          <span class="text-text-muted">/</span>
          <span>{$connectionStatus.database}</span>
        {/if}
      </div>
    {/if}
  </div>

  <!-- Right: Actions -->
  <div class="flex items-center gap-1">
    <button
      class="px-3.5 py-1.5 text-sm rounded-lg font-semibold flex items-center gap-2 transition-all {$ai.visible
        ? 'bg-accent text-white shadow-sm shadow-accent/20'
        : 'bg-accent/10 text-accent hover:bg-accent/20'}"
      on:click={toggleAiPanel}
    >
      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
        <path d="M12 2a2 2 0 0 1 2 2c0 .74-.4 1.39-1 1.73V7h1a7 7 0 0 1 7 7h1a1 1 0 0 1 1 1v3a1 1 0 0 1-1 1h-1.27A7 7 0 0 1 7.27 19H6a1 1 0 0 1-1-1v-3a1 1 0 0 1 1-1h1a7 7 0 0 1 7-7h1V5.73c-.6-.34-1-.99-1-1.73a2 2 0 0 1 2-2z"/>
        <path d="M10 17v-6"/><path d="M14 17v-6"/>
      </svg>
      AI Assistant
    </button>

    {#if $connectionStatus.connected}
      <button
        class="px-3 py-1.5 text-sm text-text-dim rounded-lg hover:bg-surface-hover hover:text-danger font-medium"
        on:click={handleDisconnect}
      >
        Disconnect
      </button>
    {/if}
  </div>
</header>
