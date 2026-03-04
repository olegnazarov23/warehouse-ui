<script lang="ts">
  import { createEventDispatcher } from "svelte";

  export let columns: string[] = [];
  export let columnTypes: string[] | undefined = undefined;
  export let rows: unknown[][] = [];
  export let editable = false;
  export let sourceTable = "";
  export let primaryKeys: string[] = [];

  const dispatch = createEventDispatcher<{ edit: { sql: string } }>();

  const ROW_HEIGHT = 28;
  const OVERSCAN = 5;

  let sortCol = -1;
  let sortDir: "asc" | "desc" = "asc";
  let scrollTop = 0;
  let containerHeight = 0;

  // Inline editing state
  let editingCell: { rowIdx: number; colIdx: number } | null = null;
  let editValue = "";

  function handleSort(idx: number) {
    if (sortCol === idx) {
      sortDir = sortDir === "asc" ? "desc" : "asc";
    } else {
      sortCol = idx;
      sortDir = "asc";
    }
  }

  $: sortedRows =
    sortCol >= 0
      ? [...rows].sort((a, b) => {
          const va = a[sortCol];
          const vb = b[sortCol];
          if (va == null && vb == null) return 0;
          if (va == null) return 1;
          if (vb == null) return -1;
          const cmp =
            typeof va === "number" && typeof vb === "number"
              ? va - vb
              : String(va).localeCompare(String(vb));
          return sortDir === "asc" ? cmp : -cmp;
        })
      : rows;

  $: totalHeight = sortedRows.length * ROW_HEIGHT;
  $: startIdx = Math.max(0, Math.floor(scrollTop / ROW_HEIGHT) - OVERSCAN);
  $: endIdx = Math.min(
    sortedRows.length,
    Math.ceil((scrollTop + containerHeight) / ROW_HEIGHT) + OVERSCAN
  );
  $: visibleRows = sortedRows.slice(startIdx, endIdx);
  $: topPad = startIdx * ROW_HEIGHT;
  $: bottomPad = Math.max(0, (sortedRows.length - endIdx) * ROW_HEIGHT);

  function handleScroll(e: Event) {
    scrollTop = (e.target as HTMLElement).scrollTop;
  }

  function formatCell(val: unknown): string {
    if (val === null || val === undefined) return "NULL";
    if (typeof val === "object") return JSON.stringify(val);
    return String(val);
  }

  function cellClass(val: unknown): string {
    if (val === null || val === undefined) return "text-text-muted italic";
    if (typeof val === "number") return "text-right tabular-nums";
    return "";
  }

  function quoteValue(val: unknown): string {
    if (val === null || val === undefined) return "NULL";
    if (typeof val === "number") return String(val);
    return `'${String(val).replace(/'/g, "''")}'`;
  }

  function handleDblClick(actualRowIdx: number, colIdx: number) {
    if (!editable || !sourceTable || primaryKeys.length === 0) return;
    editingCell = { rowIdx: actualRowIdx, colIdx };
    const val = sortedRows[actualRowIdx][colIdx];
    editValue = val === null || val === undefined ? "" : String(val);
  }

  function commitEdit() {
    if (!editingCell) return;
    const { rowIdx, colIdx } = editingCell;
    const row = sortedRows[rowIdx];
    const whereParts = primaryKeys.map((pk) => {
      const pkIdx = columns.indexOf(pk);
      return `${pk} = ${quoteValue(row[pkIdx])}`;
    });
    const sql = `UPDATE ${sourceTable}\nSET ${columns[colIdx]} = ${quoteValue(editValue)}\nWHERE ${whereParts.join(" AND ")};`;
    dispatch("edit", { sql });
    editingCell = null;
  }

  function handleEditKeydown(e: KeyboardEvent) {
    if (e.key === "Enter") {
      e.preventDefault();
      commitEdit();
    } else if (e.key === "Escape") {
      editingCell = null;
    }
  }
</script>

<div
  class="overflow-auto h-full"
  on:scroll={handleScroll}
  bind:clientHeight={containerHeight}
>
  <table class="w-full text-[13px] border-collapse">
    <thead class="sticky top-0 z-10">
      <tr class="bg-surface">
        {#each columns as col, i}
          <th
            class="px-4 py-2 text-left font-medium text-text-dim border-b border-border cursor-pointer hover:bg-surface-hover whitespace-nowrap"
            on:click={() => handleSort(i)}
          >
            <span>{col}</span>
            {#if sortCol === i}
              <span class="ml-1 text-accent">
                {sortDir === "asc" ? "▲" : "▼"}
              </span>
            {/if}
            {#if columnTypes?.[i]}
              <span class="ml-1 text-xs text-text-muted uppercase">
                {columnTypes[i]}
              </span>
            {/if}
          </th>
        {/each}
      </tr>
    </thead>
    <tbody>
      {#if topPad > 0}
        <tr><td style="height:{topPad}px" colspan={columns.length}></td></tr>
      {/if}
      {#each visibleRows as row, i (startIdx + i)}
        <tr
          class="hover:bg-surface-hover/50 {(startIdx + i) % 2 === 0
            ? ''
            : 'bg-surface/20'}"
          style="height:{ROW_HEIGHT}px"
        >
          {#each row as cell, cellIdx}
            <td
              class="px-4 py-1.5 border-b border-border/30 whitespace-nowrap max-w-xs truncate {cellClass(cell)} {editable ? 'cursor-pointer' : ''}"
              title={formatCell(cell)}
              on:dblclick={() => handleDblClick(startIdx + i, cellIdx)}
            >
              {#if editingCell && editingCell.rowIdx === startIdx + i && editingCell.colIdx === cellIdx}
                <input
                  type="text"
                  class="w-full bg-bg border border-accent rounded px-1 py-0 text-[13px] text-text outline-none"
                  bind:value={editValue}
                  on:keydown={handleEditKeydown}
                  on:blur={commitEdit}
                  autofocus
                />
              {:else}
                {formatCell(cell)}
              {/if}
            </td>
          {/each}
        </tr>
      {/each}
      {#if bottomPad > 0}
        <tr><td style="height:{bottomPad}px" colspan={columns.length}></td></tr>
      {/if}
    </tbody>
  </table>

  {#if rows.length === 0}
    <div class="text-center py-10 text-text-muted text-sm">No data</div>
  {/if}
</div>
