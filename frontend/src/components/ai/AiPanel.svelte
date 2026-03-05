<script lang="ts">
  import { onMount, onDestroy, tick } from "svelte";
  import { EventsOn, EventsOff } from "../../../wailsjs/runtime/runtime";
  import {
    ai,
    addUserMessage,
    appendChunk,
    finishStreaming,
    setConversations,
    setActiveConversation,
    addConversation,
    removeConversation,
    updateConversationTitle,
  } from "../../lib/stores/ai";
  import { discovery } from "../../lib/stores/discovery";
  import {
    aiChat,
    listAiConversations,
    getAiMessages,
    deleteAiConversation,
    getAiProviders,
    getAiSettings,
    setAiSettings,
  } from "../../lib/api";
  import { addTab, activeTab, setTabResult, setTabDryRun } from "../../lib/stores/editor";
  import { formatBytes, formatCost, formatDuration, formatNumber } from "../../lib/format";
  import type { ChatMessage, ProviderInfo } from "../../lib/types";

  let input = "";
  let messagesContainer: HTMLDivElement;
  let showChatList = false;
  let showSettings = false;

  // Settings state
  let providers: ProviderInfo[] = [];
  let settingsProvider = "";
  let settingsModel = "";
  let settingsApiKey = "";
  let settingsEndpoint = "";
  let savingSettings = false;
  let settingsError = "";
  let settingsSaved = false;
  let aiConfigured = false;

  onMount(async () => {
    EventsOn("ai:chunk", (data: any) => {
      if (data?.conversation_id === $ai.activeConversationId) {
        appendChunk(data.text);
        scrollToBottom();
      }
    });

    EventsOn("ai:title-update", (data: any) => {
      if (data?.conversation_id && data?.title) {
        updateConversationTitle(data.conversation_id, data.title);
      }
    });

    EventsOn("ai:action", (data: any) => {
      if (data?.type === "run_query" && data?.query) {
        const tabId = addTab("AI Query", data.query);
        if (data.result) {
          setTabResult(tabId, data.result);
        }
      } else if (data?.type === "dry_run" && data?.query) {
        const tabId = addTab("AI Query", data.query);
        if (data.dry_run) {
          setTabDryRun(tabId, data.dry_run);
        }
      }
    });

    // Load AI settings + providers
    try {
      providers = await getAiProviders() ?? [];
      const saved = await getAiSettings();
      if (saved) {
        // Detect "local" mode: openai provider with a non-standard endpoint
        const isLocal = saved.provider === "openai" && saved.endpoint && !saved.endpoint.includes("openai.com");
        settingsProvider = isLocal ? "local" : (saved.provider || (providers[0]?.name ?? ""));
        settingsModel = saved.model || "";
        settingsApiKey = isLocal ? "" : (saved.api_key || "");
        settingsEndpoint = saved.endpoint || "";
        aiConfigured = !!(saved.api_key || saved.endpoint);
      }
    } catch {
      // Providers not available
    }

    try {
      const convs = await listAiConversations();
      if (convs && convs.length > 0) {
        setConversations(convs);
        await switchToConversation(convs[0].id);
      }
    } catch {
      // No conversations yet
    }
  });

  onDestroy(() => {
    EventsOff("ai:chunk");
    EventsOff("ai:title-update");
    EventsOff("ai:action");
  });

  const categoryStyle: Record<string, string> = {
    explore: "bg-blue-500/10 text-blue-400",
    aggregate: "bg-purple-500/10 text-purple-400",
    trend: "bg-green-500/10 text-green-400",
    "top-n": "bg-orange-500/10 text-orange-400",
    join: "bg-pink-500/10 text-pink-400",
  };

  async function scrollToBottom() {
    await tick();
    if (messagesContainer) {
      messagesContainer.scrollTop = messagesContainer.scrollHeight;
    }
  }

  async function handleNewChat() {
    const id = crypto.randomUUID();
    const now = new Date().toISOString();
    addConversation({ id, title: "New Chat", created_at: now, updated_at: now });
    showChatList = false;
  }

  async function switchToConversation(id: string) {
    try {
      const stored = await getAiMessages(id);
      const messages: ChatMessage[] = (stored ?? []).map((m) => ({
        role: m.role as "user" | "assistant",
        content: m.content,
        timestamp: new Date(m.created_at).getTime(),
      }));
      setActiveConversation(id, messages);
    } catch {
      setActiveConversation(id, []);
    }
    showChatList = false;
    scrollToBottom();
  }

  async function handleDeleteConversation(id: string, e: MouseEvent) {
    e.stopPropagation();
    try {
      await deleteAiConversation(id);
      removeConversation(id);
    } catch (err) {
      console.error("Delete failed:", err);
    }
  }

  async function handleSaveSettings() {
    settingsError = "";
    settingsSaved = false;
    savingSettings = true;
    try {
      // "local" maps to "openai" provider with a custom endpoint
      const actualProvider = settingsProvider === "local" ? "openai" : settingsProvider;
      const actualKey = settingsProvider === "local" ? (settingsApiKey || "local") : settingsApiKey;
      await setAiSettings(actualProvider, settingsModel, actualKey, settingsEndpoint);
      settingsSaved = true;
      aiConfigured = true;
      setTimeout(() => { settingsSaved = false; }, 3000);
    } catch (e: any) {
      const msg = typeof e === "string" ? e : e?.message || "Failed to save settings";
      settingsError = msg.replace(/API key validation failed:\s*/i, "");
    } finally {
      savingSettings = false;
    }
  }

  function maskApiKey(key: string): string {
    if (!key || key.length < 8) return key ? "****" : "";
    return key.slice(0, 4) + "..." + key.slice(-4);
  }

  function buildEditorContext(): string {
    const tab = $activeTab;
    if (!tab) return "";

    const parts: string[] = [];

    if (tab.sql?.trim()) {
      parts.push(`Current SQL in editor:\n\`\`\`sql\n${tab.sql}\n\`\`\``);
    }

    if (tab.running) {
      parts.push("Status: Query is currently running...");
    }

    if (tab.error) {
      parts.push(`Query error: ${tab.error}`);
    }

    if (tab.result) {
      const r = tab.result;
      let stats = `Query results: ${formatNumber(r.row_count)} rows returned`;
      if (r.duration_ms) stats += `, took ${formatDuration(r.duration_ms)}`;
      if (r.bytes_processed != null) stats += `, scanned ${formatBytes(r.bytes_processed)}`;
      if (r.cost_usd != null) stats += `, cost ${formatCost(r.cost_usd)}`;
      if (r.cache_hit) stats += " (cache hit)";
      parts.push(stats);

      if (r.columns?.length > 0) {
        parts.push(`Result columns: ${r.columns.join(", ")}`);
      }

      // Include first few rows as sample data
      if (r.rows?.length > 0) {
        const sampleRows = r.rows.slice(0, 5);
        const preview = sampleRows.map(row =>
          r.columns.map((col, i) => `${col}=${row[i]}`).join(", ")
        ).join("\n");
        parts.push(`Sample data (first ${sampleRows.length} of ${r.row_count} rows):\n${preview}`);
      }
    }

    if (tab.dryRun) {
      const d = tab.dryRun;
      if (d.valid) {
        let dryInfo = "Dry run:";
        if (d.statement_type) dryInfo += ` ${d.statement_type}`;
        if (d.estimated_rows > 0) dryInfo += `, ~${d.estimated_rows.toLocaleString()} rows`;
        if (d.estimated_bytes > 0) dryInfo += `, ${formatBytes(d.estimated_bytes)}`;
        if (d.estimated_cost_usd > 0) dryInfo += `, ${formatCost(d.estimated_cost_usd)}`;
        if (d.referenced_tables?.length) dryInfo += `, tables: ${d.referenced_tables.join(", ")}`;
        parts.push(dryInfo);
      } else if (d.error) {
        parts.push(`Dry run error: ${d.error}`);
      }
    }

    return parts.join("\n\n");
  }

  async function handleSend() {
    const msg = input.trim();
    if (!msg || $ai.streaming) return;

    if (!$ai.activeConversationId) {
      await handleNewChat();
    }

    input = "";
    addUserMessage(msg);
    scrollToBottom();

    try {
      const context = buildEditorContext();
      await aiChat(msg, $ai.activeConversationId, context);
      finishStreaming();
    } catch (e: any) {
      const errMsg = typeof e === "string" ? e : e?.message || "AI request failed";
      appendChunk(`\n\nError: ${errMsg}`);
      finishStreaming();
    }
    scrollToBottom();
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      handleSend();
    }
  }

  function runSampleQuery(query: { title: string; sql: string }) {
    addTab(query.title, query.sql);
  }

  function extractToolActivity(content: string): { cleanContent: string; activities: string[] } {
    const activities: string[] = [];
    const cleanContent = content.replace(/<tool-activity>(.*?)<\/tool-activity>/g, (_match, msg) => {
      activities.push(msg);
      return "";
    });
    return { cleanContent, activities };
  }

  function formatContent(content: string): { type: "text" | "sql"; value: string }[] {
    // Strip any leaked tool-activity tags (from older saved messages)
    content = content.replace(/<tool-activity>.*?<\/tool-activity>/g, "").trim();
    const parts: { type: "text" | "sql"; value: string }[] = [];
    const regex = /```sql\s*([\s\S]*?)```/g;
    let lastIndex = 0;
    let match;
    while ((match = regex.exec(content)) !== null) {
      if (match.index > lastIndex) {
        parts.push({ type: "text", value: content.slice(lastIndex, match.index) });
      }
      parts.push({ type: "sql", value: match[1].trim() });
      lastIndex = regex.lastIndex;
    }
    if (lastIndex < content.length) {
      parts.push({ type: "text", value: content.slice(lastIndex) });
    }
    return parts;
  }

  function formatTime(dateStr: string): string {
    const d = new Date(dateStr);
    const now = new Date();
    const diffMs = now.getTime() - d.getTime();
    const diffMins = Math.floor(diffMs / 60000);
    if (diffMins < 1) return "just now";
    if (diffMins < 60) return `${diffMins}m ago`;
    const diffHours = Math.floor(diffMins / 60);
    if (diffHours < 24) return `${diffHours}h ago`;
    const diffDays = Math.floor(diffHours / 24);
    if (diffDays < 7) return `${diffDays}d ago`;
    return d.toLocaleDateString();
  }
</script>

<div class="flex flex-col h-full bg-surface">
  <!-- Header -->
  <div class="flex items-center justify-between px-4 py-3 border-b border-border">
    <button
      class="text-sm font-semibold flex items-center gap-1.5 hover:text-accent transition-colors truncate"
      on:click={() => { showChatList = !showChatList; showSettings = false; }}
    >
      <span class="truncate max-w-[140px]">
        {$ai.conversations.find((c) => c.id === $ai.activeConversationId)?.title ?? "AI Assistant"}
      </span>
      <svg
        width="10" height="10" viewBox="0 0 24 24" fill="none"
        stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"
        class="transition-transform shrink-0 {showChatList ? 'rotate-180' : ''}"
      >
        <path d="m6 9 6 6 6-6"/>
      </svg>
    </button>
    <div class="flex items-center gap-1">
      <button
        class="p-1.5 rounded-md transition-colors shrink-0 {showSettings ? 'text-accent bg-accent/10' : 'text-text-muted hover:text-accent hover:bg-surface-hover'}"
        on:click={() => { showSettings = !showSettings; showChatList = false; }}
        title="AI Settings"
      >
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <path d="M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z"/>
          <circle cx="12" cy="12" r="3"/>
        </svg>
      </button>
      {#if !aiConfigured}
        <span class="w-2 h-2 rounded-full bg-warning shrink-0" title="AI not configured"></span>
      {/if}
      <button
        class="text-xs text-text-muted hover:text-accent font-medium px-2 py-1 rounded-md hover:bg-surface-hover shrink-0"
        on:click={handleNewChat}
        title="New chat"
      >
        + New
      </button>
    </div>
  </div>

  <!-- Settings panel -->
  {#if showSettings}
    <div class="border-b border-border bg-bg p-4 space-y-3">
      <div class="text-xs font-semibold text-text-dim uppercase tracking-wide">AI Configuration</div>

      <!-- Provider -->
      <div>
        <label class="block text-xs text-text-muted mb-1">Provider</label>
        <div class="flex gap-1.5 flex-wrap">
          {#each providers as p}
            <button
              class="px-3 py-1.5 text-xs rounded-lg font-medium transition-all {settingsProvider === p.name
                ? 'bg-accent text-white'
                : 'bg-surface border border-border text-text-dim hover:border-accent/30'}"
              on:click={() => { settingsProvider = p.name; settingsModel = settingsModel || p.default_model; settingsEndpoint = ''; }}
            >
              {p.name === 'openai' ? 'OpenAI' : p.name === 'anthropic' ? 'Anthropic' : p.name === 'ollama' ? 'Ollama' : p.name}
            </button>
          {/each}
          <button
            class="px-3 py-1.5 text-xs rounded-lg font-medium transition-all {settingsProvider === 'local'
              ? 'bg-accent text-white'
              : 'bg-surface border border-border text-text-dim hover:border-accent/30'}"
            on:click={() => { settingsProvider = 'local'; settingsModel = ''; settingsApiKey = ''; settingsEndpoint = settingsEndpoint || 'http://localhost:1234/v1'; }}
          >
            Local
          </button>
        </div>
      </div>

      <!-- Local provider description -->
      {#if settingsProvider === 'local'}
        <div class="text-xs text-text-muted bg-surface/50 rounded-lg p-2.5 border border-border/50">
          Works with LM Studio, LocalAI, vLLM, llama.cpp, or any OpenAI-compatible server.
        </div>
      {/if}

      <!-- Endpoint (for Local / Ollama) -->
      {#if settingsProvider === 'local' || settingsProvider === 'ollama' || settingsEndpoint}
        <div>
          <label class="block text-xs text-text-muted mb-1">Endpoint URL</label>
          <input
            type="text"
            class="w-full px-3 py-2 rounded-lg bg-surface border border-border text-sm outline-none focus:border-accent/50"
            placeholder={settingsProvider === 'ollama' ? 'http://localhost:11434' : 'http://localhost:1234/v1'}
            bind:value={settingsEndpoint}
          />
        </div>
      {/if}

      <!-- API Key (hidden for local/ollama since it's optional) -->
      {#if settingsProvider !== 'local' && settingsProvider !== 'ollama'}
        <div>
          <label class="block text-xs text-text-muted mb-1">API Key</label>
          <input
            type="password"
            class="w-full px-3 py-2 rounded-lg bg-surface border border-border text-sm outline-none focus:border-accent/50"
            placeholder={settingsApiKey ? maskApiKey(settingsApiKey) : "sk-..."}
            bind:value={settingsApiKey}
          />
        </div>
      {/if}

      <!-- Model -->
      <div>
        <label class="block text-xs text-text-muted mb-1">Model {settingsProvider === 'local' ? '' : '(optional)'}</label>
        <input
          type="text"
          class="w-full px-3 py-2 rounded-lg bg-surface border border-border text-sm outline-none focus:border-accent/50"
          placeholder={settingsProvider === 'local' ? 'e.g. llama-3.1-8b' : (providers.find(p => p.name === settingsProvider)?.default_model ?? 'default')}
          bind:value={settingsModel}
        />
        {#if settingsProvider !== 'local'}
          {@const currentProv = providers.find(p => p.name === settingsProvider)}
          {#if currentProv?.min_model}
            <p class="text-[11px] text-text-muted mt-1">Minimum recommended: {currentProv.min_model}+</p>
          {/if}
        {/if}
      </div>

      <!-- Save button + status -->
      <div class="flex items-center gap-2">
        <button
          class="px-4 py-2 text-xs font-semibold bg-accent text-white rounded-lg hover:bg-accent-hover disabled:opacity-50"
          disabled={savingSettings || !settingsProvider}
          on:click={handleSaveSettings}
        >
          {savingSettings ? "Validating..." : "Save & Test"}
        </button>
        {#if settingsSaved}
          <span class="text-xs text-success flex items-center gap-1">
            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><path d="M20 6 9 17l-5-5"/></svg>
            Connected
          </span>
        {/if}
        {#if settingsError}
          <span class="text-xs text-danger">{settingsError}</span>
        {/if}
      </div>

      <div class="text-xs text-text-muted">
        {#if settingsProvider === 'local' || settingsProvider === 'ollama'}
          Runs locally — your data never leaves your machine.
        {:else}
          Keys are encrypted at rest.
        {/if}
      </div>
    </div>
  {/if}

  <!-- Chat list dropdown -->
  {#if showChatList}
    <div class="border-b border-border bg-bg max-h-64 overflow-auto">
      {#if $ai.conversations.length === 0}
        <div class="text-xs text-text-muted p-4 text-center">No conversations yet</div>
      {:else}
        {#each $ai.conversations as conv}
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <!-- svelte-ignore a11y_click_events_have_key_events -->
          <div
            class="w-full text-left px-4 py-2.5 text-sm hover:bg-surface-hover flex items-center gap-2 group cursor-pointer {conv.id === $ai.activeConversationId ? 'bg-surface-hover' : ''}"
            on:click={() => switchToConversation(conv.id)}
          >
            <span class="truncate flex-1 {conv.id === $ai.activeConversationId ? 'text-accent font-medium' : 'text-text-dim'}">
              {conv.title}
            </span>
            <span class="text-xs text-text-muted shrink-0">{formatTime(conv.updated_at)}</span>
            <button
              class="text-text-muted hover:text-danger opacity-0 group-hover:opacity-100 shrink-0 p-0.5"
              on:click={(e) => handleDeleteConversation(conv.id, e)}
              title="Delete chat"
            >
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <path d="M18 6 6 18M6 6l12 12"/>
              </svg>
            </button>
          </div>
        {/each}
      {/if}
    </div>
  {/if}

  <!-- Messages -->
  <div class="flex-1 overflow-auto p-4 space-y-4" bind:this={messagesContainer}>
    {#if !aiConfigured && $ai.messages.length === 0 && !$ai.streaming && !showSettings}
      <div class="text-center py-8 space-y-3">
        <div class="text-sm text-text-dim font-medium">AI Assistant not configured</div>
        <p class="text-xs text-text-muted">Add an API key to start chatting about your data.</p>
        <button
          class="px-4 py-2 text-xs font-semibold bg-accent text-white rounded-lg hover:bg-accent-hover"
          on:click={() => { showSettings = true; }}
        >
          Configure AI
        </button>
      </div>
    {:else if $ai.messages.length === 0 && !$ai.streaming}
      {#if $discovery.sampleQueries.length > 0}
        <div class="space-y-3">
          <div class="text-sm text-text-dim font-medium">Suggested queries based on your data</div>
          {#each $discovery.sampleQueries as query}
            <button
              class="w-full text-left p-4 rounded-xl bg-bg border border-border hover:border-accent/30 transition-all group"
              on:click={() => runSampleQuery(query)}
            >
              <div class="flex items-start justify-between gap-2 mb-1.5">
                <span class="text-sm font-medium group-hover:text-accent transition-colors">{query.title}</span>
                {#if query.category}
                  <span class="text-xs px-2 py-0.5 rounded-md font-medium shrink-0 {categoryStyle[query.category] ?? 'bg-accent/10 text-accent'}">
                    {query.category}
                  </span>
                {/if}
              </div>
              <p class="text-sm text-text-dim mb-2">{query.description}</p>
              <div class="text-xs font-mono text-text-muted bg-surface rounded-lg p-2.5 overflow-hidden">
                <div class="truncate">{query.sql}</div>
              </div>
            </button>
          {/each}
        </div>
      {:else}
        <div class="text-sm text-text-muted text-center py-10">
          Ask about your data, get SQL suggestions, or explain query results.
        </div>
      {/if}
    {/if}

    {#each $ai.messages as msg}
      <div class="{msg.role === 'user' ? 'text-text ml-8' : 'text-text mr-2'}">
        {#if msg.role === 'user'}
          <div class="p-3 rounded-xl bg-accent/10 border border-accent/15">
            <div class="text-[13px] whitespace-pre-wrap">{msg.content}</div>
          </div>
        {:else}
          <div class="space-y-2">
            {#each formatContent(msg.content) as part}
              {#if part.type === 'sql'}
                <div class="relative group/code">
                  <div class="text-xs font-mono bg-bg rounded-xl p-4 overflow-x-auto border border-border">
                    <pre class="whitespace-pre-wrap text-text">{part.value}</pre>
                  </div>
                  <button
                    class="absolute top-2 right-2 text-xs text-accent bg-surface px-2.5 py-1 rounded-lg opacity-0 group-hover/code:opacity-100 transition-opacity font-medium shadow-sm"
                    on:click={() => addTab("AI Query", part.value)}
                  >
                    Open in editor
                  </button>
                </div>
              {:else}
                <div class="text-[13px] whitespace-pre-wrap text-text/85 leading-relaxed px-1">{part.value}</div>
              {/if}
            {/each}
          </div>
        {/if}
      </div>
    {/each}

    {#if $ai.streaming}
      {@const extracted = extractToolActivity($ai.currentChunks || "")}
      <div class="mr-2">
        <div class="space-y-2">
          {#each formatContent(extracted.cleanContent) as part}
            {#if part.type === 'sql'}
              <div class="text-xs font-mono bg-bg rounded-xl p-4 overflow-x-auto border border-border">
                <pre class="whitespace-pre-wrap text-text">{part.value}</pre>
              </div>
            {:else}
              <div class="text-[13px] whitespace-pre-wrap text-text/85 leading-relaxed px-1">{part.value}</div>
            {/if}
          {/each}
          {#if extracted.activities.length > 0}
            <div class="flex items-center gap-2 text-xs text-accent/80 px-1 py-1">
              <div class="w-3.5 h-3.5 border-2 border-accent border-t-transparent rounded-full animate-spin shrink-0"></div>
              <span class="truncate">{extracted.activities[extracted.activities.length - 1]}</span>
            </div>
          {:else if !extracted.cleanContent.trim()}
            <div class="flex items-center gap-2 text-xs text-text-muted px-1">
              <div class="w-3.5 h-3.5 border-2 border-accent border-t-transparent rounded-full animate-spin"></div>
              Thinking...
            </div>
          {/if}
        </div>
      </div>
    {/if}
  </div>

  <!-- Input -->
  <div class="p-3 border-t border-border">
    <div class="flex gap-2">
      <textarea
        class="flex-1 px-3 py-2.5 rounded-xl bg-bg border border-border text-sm outline-none resize-none"
        rows="2"
        placeholder="Ask about your data..."
        bind:value={input}
        on:keydown={handleKeydown}
        disabled={$ai.streaming}
      ></textarea>
      <button
        class="self-end px-4 py-2.5 text-sm bg-accent text-white rounded-xl hover:bg-accent-hover disabled:opacity-50 font-semibold"
        disabled={$ai.streaming || !input.trim()}
        on:click={handleSend}
      >
        Send
      </button>
    </div>
  </div>
</div>
