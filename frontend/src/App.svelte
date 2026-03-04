<script lang="ts">
  import { onMount } from "svelte";
  import Shell from "./components/layout/Shell.svelte";
  import ConnectionPage from "./components/connect/ConnectionPage.svelte";
  import { connectionStatus, isConnected } from "./lib/stores/connection";
  import { getConnectionStatus } from "./lib/api";

  let ready = false;

  onMount(async () => {
    try {
      const status = await getConnectionStatus();
      connectionStatus.set(status);
    } catch {
      // Not connected or Wails not ready yet
    }
    ready = true;
  });
</script>

{#if !ready}
  <div class="flex items-center justify-center h-screen bg-bg">
    <div class="text-text-dim text-sm">Loading...</div>
  </div>
{:else if $isConnected}
  <Shell />
{:else}
  <ConnectionPage />
{/if}
