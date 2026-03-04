<script lang="ts">
  import type { ExplainNode } from "../../lib/types";

  export let node: ExplainNode;
  export let maxCost: number = 0;
  export let depth: number = 0;

  $: costWidth = maxCost > 0 && node.cost ? Math.max(4, (node.cost / maxCost) * 100) : 0;
</script>

<div class="pl-{depth > 0 ? '4' : '0'}">
  <div class="flex items-start gap-3 py-2 px-3 rounded-lg hover:bg-surface-hover/50 group">
    <!-- Cost bar -->
    {#if costWidth > 0}
      <div class="w-20 flex-shrink-0 mt-1">
        <div class="h-2 rounded-full bg-border overflow-hidden">
          <div
            class="h-full rounded-full {costWidth > 60 ? 'bg-red-500' : costWidth > 30 ? 'bg-yellow-500' : 'bg-green-500'}"
            style="width:{costWidth}%"
          ></div>
        </div>
      </div>
    {/if}

    <div class="flex-1 min-w-0">
      <!-- Operation name -->
      <div class="flex items-center gap-2">
        <span class="font-medium text-sm text-text">{node.operation}</span>
        {#if node.table}
          <span class="text-xs px-1.5 py-0.5 rounded bg-accent/15 text-accent font-mono">{node.table}</span>
        {/if}
      </div>

      <!-- Details row -->
      <div class="flex items-center gap-3 mt-0.5 text-xs text-text-muted">
        {#if node.estimated_rows}
          <span>~{node.estimated_rows.toLocaleString()} rows</span>
        {/if}
        {#if node.cost}
          <span>cost: {node.cost.toFixed(2)}</span>
        {/if}
        {#if node.details}
          <span class="truncate">{node.details}</span>
        {/if}
      </div>
    </div>
  </div>

  <!-- Children -->
  {#if node.children && node.children.length > 0}
    <div class="ml-4 border-l border-border/50 pl-1">
      {#each node.children as child}
        <svelte:self node={child} {maxCost} depth={depth + 1} />
      {/each}
    </div>
  {/if}
</div>
