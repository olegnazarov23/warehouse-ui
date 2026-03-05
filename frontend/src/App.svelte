<script lang="ts">
  import { onMount } from "svelte";
  import Shell from "./components/layout/Shell.svelte";
  import ConnectionPage from "./components/connect/ConnectionPage.svelte";
  import { connectionStatus, isConnected } from "./lib/stores/connection";
  import { clearChat, ai, setConversations, setActiveConversation } from "./lib/stores/ai";
  import { getConnectionStatus, listAiConversations, getAiMessages } from "./lib/api";
  import type { ChatMessage } from "./lib/types";

  let ready = false;
  let lastConnectionId = "";

  // Reload AI conversations when switching connections
  connectionStatus.subscribe((status) => {
    if (status.connected && status.id && status.id !== lastConnectionId) {
      lastConnectionId = status.id;
      // Reload conversations for the new connection
      loadConversationsForConnection();
    } else if (!status.connected) {
      lastConnectionId = "";
    }
  });

  async function loadConversationsForConnection() {
    clearChat();
    ai.update((s) => ({ ...s, conversations: [], activeConversationId: "" }));
    try {
      const convs = await listAiConversations();
      if (convs && convs.length > 0) {
        setConversations(convs);
        const stored = await getAiMessages(convs[0].id);
        const messages: ChatMessage[] = (stored ?? []).map((m) => ({
          role: m.role as "user" | "assistant",
          content: m.content,
          timestamp: new Date(m.created_at).getTime(),
        }));
        setActiveConversation(convs[0].id, messages);
      }
    } catch {
      // No conversations for this connection
    }
  }

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
