<script lang="ts">
  import { onMount, onDestroy } from "svelte";
  import { Chart, registerables } from "chart.js";

  Chart.register(...registerables);

  export let columns: string[] = [];
  export let columnTypes: string[] = [];
  export let rows: unknown[][] = [];

  let canvas: HTMLCanvasElement;
  let chart: Chart | null = null;
  let chartType: "bar" | "line" | "pie" = "bar";
  let mounted = false;

  // Find the first string/date column for labels
  $: labelIdx = (() => {
    if (columnTypes?.length) {
      const idx = columnTypes.findIndex((t) =>
        /char|text|varchar|string|date|timestamp|name/i.test(t)
      );
      if (idx >= 0) return idx;
    }
    // Fallback: first column where values are strings
    if (rows.length > 0) {
      for (let i = 0; i < columns.length; i++) {
        if (typeof rows[0][i] === "string") return i;
      }
    }
    return 0;
  })();

  // Find numeric columns for datasets
  $: numericIdxs = columns
    .map((_, i) => i)
    .filter((i) => {
      if (i === labelIdx) return false;
      if (rows.length === 0) return false;
      return typeof rows[0][i] === "number";
    });

  const palette = [
    "#4a6cf7", "#22c55e", "#eab308", "#ef4444", "#8b5cf6",
    "#06b6d4", "#f97316", "#ec4899", "#14b8a6", "#a855f7",
  ];

  function rebuildChart() {
    if (!canvas || rows.length === 0 || numericIdxs.length === 0) return;
    chart?.destroy();

    const displayRows = rows.slice(0, 100);
    const labels = displayRows.map((r) => String(r[labelIdx] ?? ""));
    const datasets = numericIdxs.map((i, di) => ({
      label: columns[i],
      data: displayRows.map((r) => Number(r[i]) || 0),
      backgroundColor: chartType === "pie"
        ? displayRows.map((_, ri) => palette[ri % palette.length] + "cc")
        : palette[di % palette.length] + "cc",
      borderColor: palette[di % palette.length],
      borderWidth: 1,
    }));

    chart = new Chart(canvas, {
      type: chartType,
      data: { labels, datasets },
      options: {
        responsive: true,
        maintainAspectRatio: false,
        plugins: {
          legend: { labels: { color: "#e4e4ef" } },
        },
        scales:
          chartType !== "pie"
            ? {
                x: {
                  ticks: { color: "#8888a0", maxRotation: 45 },
                  grid: { color: "#1a1a2420" },
                },
                y: {
                  ticks: { color: "#8888a0" },
                  grid: { color: "#1a1a2420" },
                },
              }
            : undefined,
      },
    });
  }

  $: if (mounted && canvas && rows.length > 0) {
    // Reactive rebuild when data or chartType changes
    chartType;
    rows;
    rebuildChart();
  }

  onMount(() => {
    mounted = true;
  });

  onDestroy(() => {
    chart?.destroy();
  });
</script>

<div class="flex flex-col h-full p-4">
  {#if numericIdxs.length === 0}
    <div class="flex items-center justify-center h-full text-text-muted text-sm">
      No numeric columns to chart. Run a query with numeric data.
    </div>
  {:else}
    <!-- Chart type pills -->
    <div class="flex gap-2 mb-3 flex-shrink-0">
      {#each ["bar", "line", "pie"] as t}
        <button
          class="px-3 py-1.5 rounded-lg text-sm font-medium transition-colors {chartType === t
            ? 'bg-accent text-white'
            : 'bg-surface text-text-dim hover:text-text hover:bg-surface-hover'}"
          on:click={() => (chartType = t)}
        >
          {t.charAt(0).toUpperCase() + t.slice(1)}
        </button>
      {/each}
      <span class="text-xs text-text-muted self-center ml-2">
        {rows.length > 100 ? "Showing first 100 rows" : `${rows.length} rows`}
      </span>
    </div>

    <!-- Chart canvas -->
    <div class="flex-1 relative min-h-0">
      <canvas bind:this={canvas}></canvas>
    </div>
  {/if}
</div>
