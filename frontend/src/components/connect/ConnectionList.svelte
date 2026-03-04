<script lang="ts">
  import { createEventDispatcher } from "svelte";
  import { savedConnections, connectionStatus } from "../../lib/stores/connection";
  import {
    connect as apiConnect,
    deleteConnection,
    listSavedConnections,
    discoverAll,
    generateSampleQueries,
  } from "../../lib/api";
  import { setDiscoveryLoading, setDiscoveryResult, setSampleQueries } from "../../lib/stores/discovery";
  import { schema } from "../../lib/stores/schema";
  import type { ConnectionConfig } from "../../lib/types";

  const dispatch = createEventDispatcher();

  let connecting = "";
  let error = "";

  const driverIcons: Record<string, string> = {
    bigquery: "BQ",
    postgres: "PG",
    mysql: "MY",
    sqlite: "SL",
    mongodb: "MG",
    clickhouse: "CH",
  };

  const driverColors: Record<string, string> = {
    bigquery: "bg-blue-500/10 text-blue-400",
    postgres: "bg-sky-500/10 text-sky-400",
    mysql: "bg-orange-500/10 text-orange-400",
    sqlite: "bg-green-500/10 text-green-400",
    mongodb: "bg-emerald-500/10 text-emerald-400",
    clickhouse: "bg-yellow-500/10 text-yellow-400",
  };

  async function handleConnect(connJson: string, id: string) {
    error = "";
    connecting = id;
    try {
      const cfg: ConnectionConfig = JSON.parse(connJson);
      const status = await apiConnect(cfg);
      connectionStatus.set(status);

      // Auto-discover after connecting from saved
      setDiscoveryLoading(true, "Discovering...");
      try {
        const result = await discoverAll();
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
        try {
          const queries = await generateSampleQueries(JSON.stringify(result));
          setSampleQueries(queries);
        } catch { /* AI not configured */ }
      } catch {
        setDiscoveryLoading(false);
      }
    } catch (e: any) {
      error = typeof e === "string" ? e : e?.message || "Connection failed";
    }
    connecting = "";
  }

  async function handleDelete(id: string) {
    try {
      await deleteConnection(id);
      const conns = await listSavedConnections();
      savedConnections.set(conns ?? []);
    } catch (e: any) {
      error = typeof e === "string" ? e : e?.message || "Delete failed";
    }
  }

  function handleEdit(connJson: string) {
    const cfg: ConnectionConfig = JSON.parse(connJson);
    dispatch("edit", cfg);
  }
</script>

<div class="space-y-3">
  {#if error}
    <div class="p-4 rounded-xl bg-danger/10 border border-danger/20 text-danger text-sm">
      {error}
    </div>
  {/if}

  {#each $savedConnections as conn (conn.id)}
    <div
      class="flex items-center gap-4 p-4 rounded-xl bg-surface border border-border hover:border-accent/30 transition-all group"
    >
      <div
        class="w-12 h-12 rounded-xl flex items-center justify-center text-sm font-bold {driverColors[conn.driver_type] ?? 'bg-accent/10 text-accent'}"
      >
        {driverIcons[conn.driver_type] ?? "??"}
      </div>

      <div class="flex-1 min-w-0">
        <div class="text-[15px] font-medium truncate">{conn.name}</div>
        <div class="text-sm text-text-dim">{conn.driver_type}</div>
      </div>

      <div class="flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
        <button
          class="px-3 py-1.5 text-sm text-text-dim hover:text-text rounded-lg hover:bg-surface-hover"
          on:click={() => handleEdit(conn.config_json)}
        >
          Edit
        </button>
        <button
          class="px-3 py-1.5 text-sm text-text-dim hover:text-danger rounded-lg hover:bg-surface-hover"
          on:click={() => handleDelete(conn.id)}
        >
          Delete
        </button>
      </div>

      <button
        class="px-5 py-2.5 text-sm font-semibold bg-accent text-white rounded-xl hover:bg-accent-hover disabled:opacity-50 shadow-sm shadow-accent/10"
        disabled={connecting === conn.id}
        on:click={() => handleConnect(conn.config_json, conn.id)}
      >
        {#if connecting === conn.id}
          <span class="flex items-center gap-2">
            <div class="w-3.5 h-3.5 border-2 border-white border-t-transparent rounded-full animate-spin"></div>
            Connecting
          </span>
        {:else}
          Connect
        {/if}
      </button>
    </div>
  {/each}

  {#if $savedConnections.length === 0}
    <p class="text-text-dim text-sm text-center py-10">
      No saved connections yet
    </p>
  {/if}

  <button
    class="w-full py-4 text-[15px] border border-dashed border-border rounded-xl text-text-dim hover:text-accent hover:border-accent/50 transition-all font-medium"
    on:click={() => dispatch("new")}
  >
    + New Connection
  </button>
</div>
