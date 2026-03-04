<script lang="ts">
  import { execute } from "../../lib/api";
  import type { QueryResult } from "../../lib/types";

  let sqlLeft = "";
  let sqlRight = "";
  let leftResult: QueryResult | null = null;
  let rightResult: QueryResult | null = null;
  let loading = false;
  let error = "";
  let keyColumn = "";

  type RowStatus = "same" | "changed" | "added" | "removed";

  interface DiffRow {
    status: RowStatus;
    left: unknown[] | null;
    right: unknown[] | null;
    changedCols: Set<number>;
  }

  let diffRows: DiffRow[] = [];
  let diffColumns: string[] = [];

  async function runDiff() {
    if (!sqlLeft.trim() || !sqlRight.trim()) return;
    loading = true;
    error = "";
    diffRows = [];

    try {
      const [left, right] = await Promise.all([
        execute(sqlLeft, 10000),
        execute(sqlRight, 10000),
      ]);
      leftResult = left;
      rightResult = right;

      // Use common columns
      diffColumns = left.columns.filter((c) => right.columns.includes(c));
      if (diffColumns.length === 0) {
        error = "No common columns between the two queries";
        loading = false;
        return;
      }

      if (!keyColumn || !diffColumns.includes(keyColumn)) {
        keyColumn = diffColumns[0];
      }

      computeDiff(left, right);
    } catch (e: any) {
      error = e?.message || String(e);
    }
    loading = false;
  }

  function computeDiff(left: QueryResult, right: QueryResult) {
    const keyIdx_L = left.columns.indexOf(keyColumn);
    const keyIdx_R = right.columns.indexOf(keyColumn);
    if (keyIdx_L < 0 || keyIdx_R < 0) return;

    // Map column indices from each result to diff column order
    const leftColMap = diffColumns.map((c) => left.columns.indexOf(c));
    const rightColMap = diffColumns.map((c) => right.columns.indexOf(c));

    // Index right rows by key
    const rightMap = new Map<string, unknown[]>();
    for (const row of right.rows) {
      const key = String(row[keyIdx_R] ?? "");
      rightMap.set(key, row);
    }

    const result: DiffRow[] = [];
    const seenKeys = new Set<string>();

    // Process left rows
    for (const lrow of left.rows) {
      const key = String(lrow[keyIdx_L] ?? "");
      seenKeys.add(key);
      const rrow = rightMap.get(key);

      if (!rrow) {
        result.push({
          status: "removed",
          left: leftColMap.map((i) => lrow[i]),
          right: null,
          changedCols: new Set(),
        });
      } else {
        const leftMapped = leftColMap.map((i) => lrow[i]);
        const rightMapped = rightColMap.map((i) => rrow[i]);
        const changed = new Set<number>();
        for (let c = 0; c < diffColumns.length; c++) {
          if (String(leftMapped[c] ?? "") !== String(rightMapped[c] ?? "")) {
            changed.add(c);
          }
        }
        result.push({
          status: changed.size > 0 ? "changed" : "same",
          left: leftMapped,
          right: rightMapped,
          changedCols: changed,
        });
      }
    }

    // Process right-only rows
    for (const rrow of right.rows) {
      const key = String(rrow[keyIdx_R] ?? "");
      if (!seenKeys.has(key)) {
        result.push({
          status: "added",
          left: null,
          right: rightColMap.map((i) => rrow[i]),
          changedCols: new Set(),
        });
      }
    }

    diffRows = result;
  }

  function fmt(val: unknown): string {
    if (val === null || val === undefined) return "NULL";
    return String(val);
  }

  function statusColor(status: RowStatus, side: "left" | "right"): string {
    if (status === "added" && side === "right") return "bg-green-500/10";
    if (status === "removed" && side === "left") return "bg-red-500/10";
    return "";
  }

  function cellColor(status: RowStatus, colIdx: number, changedCols: Set<number>): string {
    if (status === "changed" && changedCols.has(colIdx)) return "bg-yellow-500/15";
    return "";
  }

  $: stats = {
    same: diffRows.filter((r) => r.status === "same").length,
    changed: diffRows.filter((r) => r.status === "changed").length,
    added: diffRows.filter((r) => r.status === "added").length,
    removed: diffRows.filter((r) => r.status === "removed").length,
  };
</script>

<div class="flex flex-col h-full p-4 gap-3">
  <!-- SQL inputs -->
  <div class="flex gap-3 flex-shrink-0">
    <div class="flex-1">
      <label class="text-xs text-text-muted font-medium mb-1 block">Left Query</label>
      <textarea
        class="w-full h-20 px-3 py-2 rounded-lg bg-surface border border-border text-sm font-mono text-text outline-none resize-none"
        placeholder="SELECT * FROM table_v1 ..."
        bind:value={sqlLeft}
      ></textarea>
    </div>
    <div class="flex-1">
      <label class="text-xs text-text-muted font-medium mb-1 block">Right Query</label>
      <textarea
        class="w-full h-20 px-3 py-2 rounded-lg bg-surface border border-border text-sm font-mono text-text outline-none resize-none"
        placeholder="SELECT * FROM table_v2 ..."
        bind:value={sqlRight}
      ></textarea>
    </div>
  </div>

  <!-- Controls -->
  <div class="flex items-center gap-3 flex-shrink-0">
    <button
      class="px-4 py-2 text-sm bg-accent text-white rounded-lg hover:bg-accent-hover disabled:opacity-50 font-medium"
      disabled={loading || !sqlLeft.trim() || !sqlRight.trim()}
      on:click={runDiff}
    >
      {#if loading}
        <span class="flex items-center gap-1.5">
          <span class="w-3 h-3 border-2 border-white border-t-transparent rounded-full animate-spin"></span>
          Comparing...
        </span>
      {:else}
        Compare
      {/if}
    </button>

    {#if diffColumns.length > 0}
      <label class="text-xs text-text-muted">Key column:</label>
      <select
        class="px-2 py-1.5 rounded-lg bg-surface border border-border text-sm outline-none"
        bind:value={keyColumn}
        on:change={() => leftResult && rightResult && computeDiff(leftResult, rightResult)}
      >
        {#each diffColumns as col}
          <option value={col}>{col}</option>
        {/each}
      </select>
    {/if}

    {#if diffRows.length > 0}
      <div class="flex items-center gap-3 text-xs ml-auto">
        <span class="text-text-muted">{stats.same} same</span>
        <span class="text-yellow-500">{stats.changed} changed</span>
        <span class="text-green-500">{stats.added} added</span>
        <span class="text-red-500">{stats.removed} removed</span>
      </div>
    {/if}
  </div>

  {#if error}
    <div class="p-3 rounded-lg bg-danger/10 border border-danger/20 text-danger text-sm">
      {error}
    </div>
  {/if}

  <!-- Diff table -->
  {#if diffRows.length > 0}
    <div class="flex-1 overflow-auto min-h-0 border border-border rounded-lg">
      <table class="w-full text-[13px] border-collapse">
        <thead class="sticky top-0 z-10">
          <tr class="bg-surface">
            <th class="px-3 py-2 text-left font-medium text-text-muted border-b border-border text-xs w-12">
              &Delta;
            </th>
            {#each diffColumns as col}
              <th class="px-3 py-2 text-left font-medium text-text-dim border-b border-border whitespace-nowrap">
                {col}
              </th>
            {/each}
          </tr>
        </thead>
        <tbody>
          {#each diffRows as row}
            {#if row.status === "same"}
              <tr class="text-text-muted">
                <td class="px-3 py-1 border-b border-border/30 text-xs">=</td>
                {#each diffColumns as _, ci}
                  <td class="px-3 py-1 border-b border-border/30 whitespace-nowrap truncate max-w-xs">
                    {fmt(row.left?.[ci])}
                  </td>
                {/each}
              </tr>
            {:else if row.status === "changed"}
              <tr class="bg-yellow-500/5">
                <td class="px-3 py-1 border-b border-border/30 text-xs text-yellow-500 font-bold" rowspan="2">~</td>
                {#each diffColumns as _, ci}
                  <td class="px-3 py-1 border-b border-border/10 whitespace-nowrap truncate max-w-xs {cellColor(row.status, ci, row.changedCols)}">
                    {fmt(row.left?.[ci])}
                  </td>
                {/each}
              </tr>
              <tr class="bg-yellow-500/5">
                {#each diffColumns as _, ci}
                  <td class="px-3 py-1 border-b border-border/30 whitespace-nowrap truncate max-w-xs {cellColor(row.status, ci, row.changedCols)}">
                    {fmt(row.right?.[ci])}
                  </td>
                {/each}
              </tr>
            {:else if row.status === "added"}
              <tr class="bg-green-500/10">
                <td class="px-3 py-1 border-b border-border/30 text-xs text-green-500 font-bold">+</td>
                {#each diffColumns as _, ci}
                  <td class="px-3 py-1 border-b border-border/30 whitespace-nowrap truncate max-w-xs">
                    {fmt(row.right?.[ci])}
                  </td>
                {/each}
              </tr>
            {:else if row.status === "removed"}
              <tr class="bg-red-500/10">
                <td class="px-3 py-1 border-b border-border/30 text-xs text-red-500 font-bold">-</td>
                {#each diffColumns as _, ci}
                  <td class="px-3 py-1 border-b border-border/30 whitespace-nowrap truncate max-w-xs">
                    {fmt(row.left?.[ci])}
                  </td>
                {/each}
              </tr>
            {/if}
          {/each}
        </tbody>
      </table>
    </div>
  {:else if !loading && !error}
    <div class="flex-1 flex items-center justify-center text-text-muted text-sm">
      Enter two SQL queries and click Compare
    </div>
  {/if}
</div>
