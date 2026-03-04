<script lang="ts">
  import { createEventDispatcher } from "svelte";
  import {
    connect as apiConnect,
    disconnect as apiDisconnect,
    saveConnection,
    listSavedConnections,
    pickFile,
    pickFolder,
    setCodePaths,
    scanCodeContext,
    parseConnectionString,
    testConnection,
    listServerDatabases,
    discoverAll,
    generateSampleQueries,
  } from "../../lib/api";
  import { connectionStatus, savedConnections, emptyConfig } from "../../lib/stores/connection";
  import { setDiscoveryLoading, setDiscoveryResult, setSampleQueries } from "../../lib/stores/discovery";
  import { schema } from "../../lib/stores/schema";
  import type { ConnectionConfig, DriverType } from "../../lib/types";

  export let config: ConnectionConfig | null = null;

  const dispatch = createEventDispatcher();

  const drivers: { value: DriverType; label: string }[] = [
    { value: "bigquery", label: "BigQuery" },
    { value: "postgres", label: "PostgreSQL" },
    { value: "mysql", label: "MySQL" },
    { value: "sqlite", label: "SQLite" },
    { value: "mongodb", label: "MongoDB" },
    { value: "clickhouse", label: "ClickHouse" },
  ];

  let form: ConnectionConfig = config ? { ...config } : emptyConfig();
  let error = "";
  let discovering = false;
  let discoveryProgress = "";
  let testing = false;
  let discoveryCancelled = false;

  let bqCredPath = form.options?.credentials_path ?? "";

  // Code repository paths for AI context
  let codePaths: string[] = (() => {
    try {
      const raw = form.options?.code_paths;
      return raw ? JSON.parse(raw) : [];
    } catch { return []; }
  })();

  // Detected connections from code scanning
  type DetectedConn = {
    source: string;
    driver_hint: string;
    detail: string;
    status: "idle" | "testing" | "success" | "failed" | "added";
    error?: string;
  };
  let detectedConns: DetectedConn[] = [];
  let scanning = false;

  // Database discovery
  let serverDatabases: string[] = [];
  let discoveringDbs = false;

  async function handleDiscoverDatabases() {
    discoveringDbs = true;
    error = "";
    try {
      const dbs = await listServerDatabases(form);
      serverDatabases = dbs ?? [];
      if (serverDatabases.length > 0 && !form.database) {
        form.database = serverDatabases[0];
      }
    } catch (e: any) {
      error = typeof e === "string" ? e : e?.message || "Failed to discover databases";
    }
    discoveringDbs = false;
  }

  $: {
    if (form.type === "bigquery") {
      form.options = { ...form.options, credentials_path: bqCredPath };
    }
  }

  // Sync code paths into form options
  $: {
    form.options = { ...form.options, code_paths: JSON.stringify(codePaths) };
  }

  async function handlePickFile() {
    const path = await pickFile("Select Service Account JSON");
    if (path) {
      bqCredPath = path;
    }
  }

  async function handleAddCodePath() {
    const path = await pickFolder("Select Code Repository");
    if (path && !codePaths.includes(path)) {
      codePaths = [...codePaths, path];
    }
  }

  function removeCodePath(path: string) {
    codePaths = codePaths.filter(p => p !== path);
  }

  async function handleScanCode() {
    if (codePaths.length === 0) return;
    scanning = true;
    detectedConns = [];
    try {
      const ctx = await scanCodeContext(codePaths);
      if (ctx?.detected_connections?.length > 0) {
        detectedConns = ctx.detected_connections.map((dc: any) => ({
          source: dc.source,
          driver_hint: dc.driver_hint,
          detail: dc.detail,
          status: "idle" as const,
        }));
      }
    } catch {
      // scan failed silently — not critical
    }
    scanning = false;
  }

  async function handleTestDetected(index: number) {
    const conn = detectedConns[index];
    detectedConns[index] = { ...conn, status: "testing", error: undefined };
    detectedConns = detectedConns;
    try {
      const cfg = await parseConnectionString(conn.detail, conn.driver_hint);
      await testConnection(cfg);
      detectedConns[index] = { ...conn, status: "success" };
    } catch (e: any) {
      detectedConns[index] = {
        ...conn,
        status: "failed",
        error: typeof e === "string" ? e : e?.message || "Connection failed",
      };
    }
    detectedConns = detectedConns;
  }

  async function handleAddDetected(index: number) {
    const conn = detectedConns[index];
    try {
      const cfg = await parseConnectionString(conn.detail, conn.driver_hint);
      await saveConnection(cfg);
      const conns = await listSavedConnections();
      savedConnections.set(conns ?? []);
      detectedConns[index] = { ...conn, status: "added" };
    } catch (e: any) {
      detectedConns[index] = {
        ...conn,
        status: "failed",
        error: typeof e === "string" ? e : e?.message || "Failed to add",
      };
    }
    detectedConns = detectedConns;
  }

  async function handleTestAndAddDetected(index: number) {
    const conn = detectedConns[index];
    detectedConns[index] = { ...conn, status: "testing", error: undefined };
    detectedConns = detectedConns;
    try {
      const cfg = await parseConnectionString(conn.detail, conn.driver_hint);
      await testConnection(cfg);
      await saveConnection(cfg);
      const conns = await listSavedConnections();
      savedConnections.set(conns ?? []);
      detectedConns[index] = { ...conn, status: "added" };
    } catch (e: any) {
      detectedConns[index] = {
        ...conn,
        status: "failed",
        error: typeof e === "string" ? e : e?.message || "Connection failed",
      };
    }
    detectedConns = detectedConns;
  }

  async function handleTest() {
    error = "";
    testing = true;
    try {
      const status = await apiConnect(form);
      connectionStatus.set(status);
    } catch (e: any) {
      error = typeof e === "string" ? e : e?.message || "Connection failed";
    }
    testing = false;
  }

  async function handleCancelDiscovery() {
    discoveryCancelled = true;
    try { await apiDisconnect(); } catch {}
    discovering = false;
    discoveryProgress = "";
    setDiscoveryLoading(false);
    connectionStatus.set({ connected: false, driver: "", database: "" } as any);
  }

  async function handleDiscoverAndConnect() {
    error = "";
    discovering = true;
    discoveryCancelled = false;
    discoveryProgress = "Connecting...";
    setDiscoveryLoading(true, "Connecting...");

    try {
      await saveConnection(form);

      if (discoveryCancelled) return;
      discoveryProgress = "Establishing connection...";
      setDiscoveryLoading(true, discoveryProgress);
      const status = await apiConnect(form);
      if (discoveryCancelled) return;
      connectionStatus.set(status);

      // Set code paths for AI context and scan for detected connections
      if (codePaths.length > 0) {
        await setCodePaths(codePaths);
        if (discoveryCancelled) return;
        discoveryProgress = `Scanning ${codePaths.length} code repo${codePaths.length > 1 ? "s" : ""}...`;
        setDiscoveryLoading(true, discoveryProgress);

        // Scan code repos for DB connections (non-blocking — don't fail discovery)
        try {
          const ctx = await scanCodeContext(codePaths);
          if (discoveryCancelled) return;
          if (ctx?.detected_connections?.length > 0) {
            detectedConns = ctx.detected_connections.map((dc: any) => ({
              source: dc.source,
              driver_hint: dc.driver_hint,
              detail: dc.detail,
              status: "idle" as const,
            }));
          }
        } catch {
          // scan failed silently
        }
      }

      if (discoveryCancelled) return;
      discoveryProgress = "Discovering datasets and tables...";
      setDiscoveryLoading(true, discoveryProgress);
      const result = await discoverAll();
      if (discoveryCancelled) return;
      setDiscoveryResult(result);

      if (result.databases.length > 0) {
        const dbNames = result.databases.map(d => d.name);
        schema.update(s => ({
          ...s,
          databases: dbNames,
          activeDatabase: dbNames[0],
          tables: result.databases[0].tables,
        }));
      }

      discoveryProgress = `Found ${result.total_tables} tables across ${result.databases.length} datasets`;
      setDiscoveryLoading(true, discoveryProgress);

      if (discoveryCancelled) return;
      try {
        const queries = await generateSampleQueries(JSON.stringify(result));
        setSampleQueries(queries);
      } catch {
        // AI not configured — fine
      }

      const conns = await listSavedConnections();
      savedConnections.set(conns ?? []);

    } catch (e: any) {
      if (discoveryCancelled) return;
      error = typeof e === "string" ? e : e?.message || "Discovery failed";
      setDiscoveryLoading(false);
    }
    discovering = false;
  }
</script>

<div class="space-y-5">
  <button
    class="text-sm text-text-dim hover:text-accent flex items-center gap-1"
    on:click={() => dispatch("back")}
  >
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
      <path d="M15 18l-6-6 6-6"/>
    </svg>
    Back
  </button>

  {#if error}
    <div class="p-4 rounded-xl bg-danger/10 border border-danger/20 text-danger text-sm">
      {error}
    </div>
  {/if}

  <!-- Driver type as pill selector -->
  <div>
    <label class="block text-sm text-text-dim mb-2 font-medium">Database Type</label>
    <div class="flex gap-2 flex-wrap">
      {#each drivers as d}
        <button
          class="px-4 py-2.5 rounded-lg text-sm font-medium transition-all
            {form.type === d.value
            ? 'bg-accent text-white shadow-md shadow-accent/20'
            : 'bg-surface text-text-dim hover:bg-surface-hover hover:text-text'}"
          on:click={() => (form = emptyConfig(d.value))}
        >
          {d.label}
        </button>
      {/each}
    </div>
  </div>

  <!-- Name -->
  <div>
    <label class="block text-sm text-text-dim mb-2 font-medium">Connection Name</label>
    <input
      type="text"
      class="w-full px-4 py-3 rounded-xl bg-surface border border-border text-sm outline-none"
      placeholder="My Database"
      bind:value={form.name}
    />
  </div>

  <!-- Driver-specific fields -->
  {#if form.type === "bigquery"}
    <div>
      <label class="block text-sm text-text-dim mb-2 font-medium">Project ID</label>
      <input
        type="text"
        class="w-full px-4 py-3 rounded-xl bg-surface border border-border text-sm outline-none"
        placeholder="my-gcp-project (auto-detected from SA JSON)"
        bind:value={form.database}
      />
    </div>
    <div>
      <label class="block text-sm text-text-dim mb-2 font-medium">Service Account JSON</label>
      <div class="flex gap-2">
        <input
          type="text"
          class="flex-1 px-4 py-3 rounded-xl bg-surface border border-border text-sm outline-none"
          placeholder="/path/to/service-account.json"
          bind:value={bqCredPath}
        />
        <button
          class="px-5 py-3 text-sm bg-surface border border-border rounded-xl hover:bg-surface-hover font-medium"
          on:click={handlePickFile}
        >
          Browse
        </button>
      </div>
    </div>

  {:else if form.type === "sqlite"}
    <div>
      <label class="block text-sm text-text-dim mb-2 font-medium">Database File</label>
      <div class="flex gap-2">
        <input
          type="text"
          class="flex-1 px-4 py-3 rounded-xl bg-surface border border-border text-sm outline-none"
          placeholder="/path/to/database.db"
          bind:value={form.database}
        />
        <button
          class="px-5 py-3 text-sm bg-surface border border-border rounded-xl hover:bg-surface-hover font-medium"
          on:click={async () => {
            const path = await pickFile("Select SQLite Database");
            if (path) form.database = path;
          }}
        >
          Browse
        </button>
      </div>
    </div>

  {:else}
    <div>
      <label class="block text-sm text-text-dim mb-2 font-medium">Host</label>
      <input
        type="text"
        class="w-full px-4 py-3 rounded-xl bg-surface border border-border text-sm outline-none"
        placeholder={form.type === "postgres"
          ? "localhost:5432"
          : form.type === "mysql"
            ? "localhost:3306"
            : form.type === "mongodb"
              ? "localhost:27017"
              : "localhost:9000"}
        bind:value={form.host}
      />
    </div>
    <div>
      <label class="block text-sm text-text-dim mb-2 font-medium">Database</label>
      <div class="flex gap-2">
        {#if serverDatabases.length > 0}
          <select
            class="flex-1 px-4 py-3 rounded-xl bg-surface border border-border text-sm outline-none"
            bind:value={form.database}
          >
            {#each serverDatabases as db}
              <option value={db}>{db}</option>
            {/each}
          </select>
        {:else}
          <input
            type="text"
            class="flex-1 px-4 py-3 rounded-xl bg-surface border border-border text-sm outline-none"
            placeholder="Leave empty to auto-detect"
            bind:value={form.database}
          />
        {/if}
        <button
          class="px-4 py-3 text-sm bg-surface border border-border rounded-xl hover:bg-surface-hover font-medium disabled:opacity-50 flex items-center gap-1.5"
          disabled={discoveringDbs || !form.host}
          on:click={handleDiscoverDatabases}
        >
          {#if discoveringDbs}
            <div class="w-3.5 h-3.5 border-2 border-accent border-t-transparent rounded-full animate-spin"></div>
          {:else}
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
              <circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/>
            </svg>
          {/if}
          Discover
        </button>
      </div>
    </div>
    <div class="grid grid-cols-2 gap-3">
      <div>
        <label class="block text-sm text-text-dim mb-2 font-medium">Username</label>
        <input
          type="text"
          class="w-full px-4 py-3 rounded-xl bg-surface border border-border text-sm outline-none"
          bind:value={form.username}
        />
      </div>
      <div>
        <label class="block text-sm text-text-dim mb-2 font-medium">Password</label>
        <input
          type="password"
          class="w-full px-4 py-3 rounded-xl bg-surface border border-border text-sm outline-none"
          bind:value={form.password}
        />
      </div>
    </div>
    <div>
      <label class="block text-sm text-text-dim mb-2 font-medium">SSL Mode</label>
      <select
        class="w-full px-4 py-3 rounded-xl bg-surface border border-border text-sm outline-none"
        bind:value={form.ssl_mode}
      >
        <option value="disable">Disable</option>
        <option value="require">Require</option>
        <option value="verify-full">Verify Full</option>
      </select>
    </div>

    {#if form.type === "mongodb"}
      <div class="grid grid-cols-2 gap-3">
        <div>
          <label class="block text-sm text-text-dim mb-2 font-medium">Auth Source</label>
          <input
            type="text"
            class="w-full px-4 py-3 rounded-xl bg-surface border border-border text-sm outline-none"
            placeholder="database name (default)"
            value={form.options?.auth_source ?? ""}
            on:input={(e) => {
              form.options = { ...form.options, auth_source: e.currentTarget.value };
            }}
          />
        </div>
        <div>
          <label class="block text-sm text-text-dim mb-2 font-medium">Replica Set</label>
          <input
            type="text"
            class="w-full px-4 py-3 rounded-xl bg-surface border border-border text-sm outline-none"
            placeholder="optional"
            value={form.options?.replica_set ?? ""}
            on:input={(e) => {
              form.options = { ...form.options, replica_set: e.currentTarget.value };
            }}
          />
        </div>
      </div>
    {/if}
  {/if}

  <!-- SSH Tunnel (for network databases) -->
  {#if form.type !== "bigquery" && form.type !== "sqlite"}
    <div>
      <button
        class="flex items-center gap-2 text-sm text-text-dim hover:text-accent font-medium"
        on:click={() => {
          const cur = form.options?.ssh_host ?? "";
          if (cur) {
            // Clear SSH settings
            const { ssh_host, ssh_user, ssh_password, ssh_key_path, ...rest } = form.options ?? {};
            form.options = rest;
          } else {
            form.options = { ...form.options, ssh_host: "" };
          }
          form = form;
        }}
      >
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
          {#if form.options?.ssh_host !== undefined}
            <path d="M6 9l6 6 6-6"/>
          {:else}
            <path d="M9 18l6-6-6-6"/>
          {/if}
        </svg>
        SSH Tunnel
        {#if form.options?.ssh_host}
          <span class="px-1.5 py-0.5 rounded text-[10px] font-semibold bg-accent/10 text-accent">ON</span>
        {/if}
      </button>

      {#if form.options?.ssh_host !== undefined}
        <div class="mt-3 space-y-3 pl-4 border-l-2 border-border">
          <div>
            <label class="block text-sm text-text-dim mb-2 font-medium">SSH Host</label>
            <input
              type="text"
              class="w-full px-4 py-3 rounded-xl bg-surface border border-border text-sm outline-none"
              placeholder="bastion.example.com or bastion.example.com:22"
              value={form.options?.ssh_host ?? ""}
              on:input={(e) => {
                form.options = { ...form.options, ssh_host: e.currentTarget.value };
              }}
            />
          </div>
          <div>
            <label class="block text-sm text-text-dim mb-2 font-medium">Jump Host (ProxyJump)</label>
            <input
              type="text"
              class="w-full px-4 py-3 rounded-xl bg-surface border border-border text-sm outline-none"
              placeholder="user@bastion (optional, -J equivalent)"
              value={form.options?.ssh_jump_host ?? ""}
              on:input={(e) => {
                form.options = { ...form.options, ssh_jump_host: e.currentTarget.value };
              }}
            />
          </div>
          <div>
            <label class="block text-sm text-text-dim mb-2 font-medium">SSH User</label>
            <input
              type="text"
              class="w-full px-4 py-3 rounded-xl bg-surface border border-border text-sm outline-none"
              placeholder="ubuntu"
              value={form.options?.ssh_user ?? ""}
              on:input={(e) => {
                form.options = { ...form.options, ssh_user: e.currentTarget.value };
              }}
            />
          </div>
          <div class="grid grid-cols-2 gap-3">
            <div>
              <label class="block text-sm text-text-dim mb-2 font-medium">SSH Key File</label>
              <div class="flex gap-2">
                <input
                  type="text"
                  class="flex-1 px-4 py-3 rounded-xl bg-surface border border-border text-sm outline-none"
                  placeholder="~/.ssh/id_rsa (auto-detected)"
                  value={form.options?.ssh_key_path ?? ""}
                  on:input={(e) => {
                    form.options = { ...form.options, ssh_key_path: e.currentTarget.value };
                  }}
                />
                <button
                  class="px-4 py-3 text-sm bg-surface border border-border rounded-xl hover:bg-surface-hover font-medium"
                  on:click={async () => {
                    const path = await pickFile("Select SSH Key");
                    if (path) form.options = { ...form.options, ssh_key_path: path };
                  }}
                >
                  Browse
                </button>
              </div>
            </div>
            <div>
              <label class="block text-sm text-text-dim mb-2 font-medium">SSH Password</label>
              <input
                type="password"
                class="w-full px-4 py-3 rounded-xl bg-surface border border-border text-sm outline-none"
                placeholder="Key passphrase or password"
                value={form.options?.ssh_password ?? ""}
                on:input={(e) => {
                  form.options = { ...form.options, ssh_password: e.currentTarget.value };
                }}
              />
            </div>
          </div>
          <p class="text-xs text-text-muted">
            Leave key file empty to auto-detect ~/.ssh/id_rsa, id_ed25519, or id_ecdsa.
          </p>
        </div>
      {/if}
    </div>
  {/if}

  <!-- Code Repositories (AI Context) -->
  <div>
    <label class="block text-sm text-text-dim mb-2 font-medium">Code Repositories (optional)</label>
    <p class="text-xs text-text-muted mb-3">
      Link local codebases so AI can learn how your database is used — query patterns, ORM models, migrations.
    </p>

    {#if codePaths.length > 0}
      <div class="space-y-2 mb-3">
        {#each codePaths as path}
          <div class="flex items-center gap-2 px-3 py-2.5 rounded-lg bg-surface border border-border text-sm group">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" class="text-text-muted shrink-0">
              <path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/>
            </svg>
            <span class="flex-1 truncate text-text-dim font-mono text-xs">{path}</span>
            <button
              class="text-text-muted hover:text-danger opacity-0 group-hover:opacity-100 transition-opacity text-xs font-medium"
              on:click={() => removeCodePath(path)}
            >
              remove
            </button>
          </div>
        {/each}
      </div>
    {/if}

    <button
      class="flex items-center gap-2 px-4 py-2.5 text-sm border border-dashed border-border rounded-xl text-text-dim hover:text-accent hover:border-accent/30 transition-all"
      on:click={handleAddCodePath}
    >
      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
        <line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/>
      </svg>
      Add folder
    </button>
  </div>

  <!-- Discovery progress -->
  {#if discovering && discoveryProgress}
    <div class="flex items-center gap-3 p-4 rounded-xl bg-accent/5 border border-accent/20">
      <div class="w-5 h-5 border-2 border-accent border-t-transparent rounded-full animate-spin"></div>
      <span class="text-sm text-accent flex-1">{discoveryProgress}</span>
      <button
        class="px-3 py-1.5 text-xs font-medium text-danger bg-danger/10 rounded-lg hover:bg-danger/20 transition-colors"
        on:click={handleCancelDiscovery}
      >
        Cancel
      </button>
    </div>
  {/if}

  <!-- Detected Connections from Code Scanning -->
  {#if detectedConns.length > 0}
    <div>
      <label class="block text-sm text-text-dim mb-2 font-medium">Detected Connections</label>
      <p class="text-xs text-text-muted mb-3">
        Found database connections in your code. Test and add them as saved connections.
      </p>
      <div class="space-y-2">
        {#each detectedConns as conn, i}
          <div class="flex items-center gap-3 px-3 py-3 rounded-lg bg-surface border border-border text-sm">
            <!-- Status icon -->
            <div class="shrink-0 w-5 flex justify-center">
              {#if conn.status === "testing"}
                <div class="w-4 h-4 border-2 border-accent border-t-transparent rounded-full animate-spin"></div>
              {:else if conn.status === "success"}
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="#22c55e" stroke-width="2.5" stroke-linecap="round"><path d="M20 6L9 17l-5-5"/></svg>
              {:else if conn.status === "added"}
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="#22c55e" stroke-width="2.5" stroke-linecap="round"><path d="M20 6L9 17l-5-5"/></svg>
              {:else if conn.status === "failed"}
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="#ef4444" stroke-width="2.5" stroke-linecap="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
              {:else}
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" class="text-text-muted">
                  <ellipse cx="12" cy="5" rx="9" ry="3"/><path d="M3 5v14c0 1.66 4 3 9 3s9-1.34 9-3V5"/><path d="M3 12c0 1.66 4 3 9 3s9-1.34 9-3"/>
                </svg>
              {/if}
            </div>

            <!-- Info -->
            <div class="flex-1 min-w-0">
              <div class="flex items-center gap-2">
                {#if conn.driver_hint}
                  <span class="px-1.5 py-0.5 text-[10px] font-semibold uppercase rounded bg-accent/10 text-accent">
                    {conn.driver_hint}
                  </span>
                {/if}
                <span class="text-xs text-text-muted truncate">{conn.source}</span>
              </div>
              <div class="font-mono text-xs text-text-dim truncate mt-0.5">{conn.detail}</div>
              {#if conn.status === "failed" && conn.error}
                <div class="text-xs text-danger mt-1">{conn.error}</div>
              {/if}
              {#if conn.status === "added"}
                <div class="text-xs text-green-400 mt-1">Added to saved connections</div>
              {/if}
            </div>

            <!-- Actions -->
            <div class="shrink-0 flex gap-2">
              {#if conn.status === "idle" || conn.status === "failed"}
                <button
                  class="px-3 py-1.5 text-xs bg-accent/10 text-accent rounded-lg hover:bg-accent/20 font-medium transition-colors"
                  on:click={() => handleTestAndAddDetected(i)}
                >
                  Test & Add
                </button>
              {:else if conn.status === "success"}
                <button
                  class="px-3 py-1.5 text-xs bg-green-500/10 text-green-400 rounded-lg hover:bg-green-500/20 font-medium transition-colors"
                  on:click={() => handleAddDetected(i)}
                >
                  Add
                </button>
              {:else if conn.status === "added"}
                <span class="text-xs text-text-muted">Saved</span>
              {/if}
            </div>
          </div>
        {/each}
      </div>
    </div>
  {/if}

  <!-- Scan code button (when repos are set but not yet discovered) -->
  {#if codePaths.length > 0 && detectedConns.length === 0 && !discovering}
    <button
      class="flex items-center gap-2 px-4 py-2.5 text-sm border border-border rounded-xl text-text-dim hover:text-accent hover:border-accent/30 transition-all disabled:opacity-50"
      disabled={scanning}
      on:click={handleScanCode}
    >
      {#if scanning}
        <div class="w-4 h-4 border-2 border-accent border-t-transparent rounded-full animate-spin"></div>
        Scanning code...
      {:else}
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/>
        </svg>
        Scan for database connections
      {/if}
    </button>
  {/if}

  <!-- Actions -->
  <div class="flex gap-3 pt-2">
    <button
      class="flex-1 py-3.5 text-[15px] bg-accent text-white rounded-xl hover:bg-accent-hover disabled:opacity-50 font-semibold shadow-lg shadow-accent/20"
      disabled={discovering}
      on:click={handleDiscoverAndConnect}
    >
      {#if discovering}
        <span class="flex items-center justify-center gap-2">
          <div class="w-4 h-4 border-2 border-white border-t-transparent rounded-full animate-spin"></div>
          Discovering...
        </span>
      {:else}
        Discover & Connect
      {/if}
    </button>
    <button
      class="px-6 py-3.5 text-[15px] border border-border rounded-xl text-text-dim hover:text-text hover:bg-surface-hover disabled:opacity-50 font-medium"
      disabled={testing}
      on:click={handleTest}
    >
      {testing ? "Testing..." : "Test"}
    </button>
  </div>
</div>
