<script lang="ts">
  export let columns: string[] = [];
  export let columnTypes: string[] | undefined = undefined;
  export let rows: unknown[][] = [];

  let sortCol = -1;
  let sortDir: "asc" | "desc" = "asc";

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
</script>

<div class="overflow-auto h-full">
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
      {#each sortedRows as row, rowIdx}
        <tr class="hover:bg-surface-hover/50 {rowIdx % 2 === 0 ? '' : 'bg-surface/20'}">
          {#each row as cell, cellIdx}
            <td
              class="px-4 py-1.5 border-b border-border/30 whitespace-nowrap max-w-xs truncate {cellClass(
                cell
              )}"
              title={formatCell(cell)}
            >
              {formatCell(cell)}
            </td>
          {/each}
        </tr>
      {/each}
    </tbody>
  </table>

  {#if rows.length === 0}
    <div class="text-center py-10 text-text-muted text-sm">No data</div>
  {/if}
</div>
