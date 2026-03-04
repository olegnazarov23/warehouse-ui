<script lang="ts">
  import Header from "./Header.svelte";
  import Sidebar from "./Sidebar.svelte";
  import QueryEditor from "../editor/QueryEditor.svelte";
  import EditorTabs from "../editor/EditorTabs.svelte";
  import Toolbar from "../editor/Toolbar.svelte";
  import ResultsPanel from "../results/ResultsPanel.svelte";
  import AiPanel from "../ai/AiPanel.svelte";
  import { ai } from "../../lib/stores/ai";
  import { activeTab } from "../../lib/stores/editor";

  let sidebarWidth = 260;
  let editorHeight = 45; // percent
  let aiPanelWidth = 320;

  // --- Drag state ---
  let dragging: "sidebar" | "editor" | "ai" | null = null;
  let dragStartX = 0;
  let dragStartY = 0;
  let dragStartValue = 0;
  let containerRect: DOMRect | null = null;
  let editorContainerEl: HTMLElement;

  function onSidebarHandleDown(e: MouseEvent) {
    e.preventDefault();
    dragging = "sidebar";
    dragStartX = e.clientX;
    dragStartValue = sidebarWidth;
    addDragListeners();
  }

  function onEditorHandleDown(e: MouseEvent) {
    e.preventDefault();
    dragging = "editor";
    dragStartY = e.clientY;
    dragStartValue = editorHeight;
    if (editorContainerEl) {
      containerRect = editorContainerEl.getBoundingClientRect();
    }
    addDragListeners();
  }

  function onAiHandleDown(e: MouseEvent) {
    e.preventDefault();
    dragging = "ai";
    dragStartX = e.clientX;
    dragStartValue = aiPanelWidth;
    addDragListeners();
  }

  function onMouseMove(e: MouseEvent) {
    if (!dragging) return;

    if (dragging === "sidebar") {
      const delta = e.clientX - dragStartX;
      sidebarWidth = Math.max(180, Math.min(500, dragStartValue + delta));
    } else if (dragging === "editor" && containerRect) {
      const containerH = containerRect.height;
      const relativeY = e.clientY - containerRect.top;
      editorHeight = Math.max(15, Math.min(85, (relativeY / containerH) * 100));
    } else if (dragging === "ai") {
      const delta = dragStartX - e.clientX; // reversed: dragging left = wider
      aiPanelWidth = Math.max(240, Math.min(600, dragStartValue + delta));
    }
  }

  function onMouseUp() {
    dragging = null;
    containerRect = null;
    removeDragListeners();
  }

  function addDragListeners() {
    window.addEventListener("mousemove", onMouseMove);
    window.addEventListener("mouseup", onMouseUp);
  }

  function removeDragListeners() {
    window.removeEventListener("mousemove", onMouseMove);
    window.removeEventListener("mouseup", onMouseUp);
  }
</script>

<div class="flex flex-col h-screen" class:select-none={!!dragging}>
  <Header />

  <div class="flex flex-1 overflow-hidden">
    <!-- Sidebar -->
    <div
      class="flex-shrink-0 border-r border-border overflow-hidden"
      style="width: {sidebarWidth}px"
    >
      <Sidebar />
    </div>

    <!-- Sidebar resize handle -->
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div
      class="w-1 flex-shrink-0 cursor-col-resize hover:bg-accent transition-colors {dragging === 'sidebar' ? 'bg-accent' : 'bg-border'}"
      on:mousedown={onSidebarHandleDown}
    ></div>

    <!-- Main content -->
    <div class="flex-1 flex flex-col overflow-hidden min-w-0">
      <EditorTabs />
      <Toolbar />

      <!-- Editor + Results split -->
      <div class="flex-1 flex flex-col overflow-hidden" bind:this={editorContainerEl}>
        <div
          class="overflow-hidden"
          style="height: {editorHeight}%"
        >
          <QueryEditor />
        </div>

        <!-- Editor/Results resize handle -->
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div
          class="h-1 flex-shrink-0 cursor-row-resize hover:bg-accent transition-colors {dragging === 'editor' ? 'bg-accent' : 'bg-border'}"
          on:mousedown={onEditorHandleDown}
        ></div>

        <div class="flex-1 overflow-hidden">
          <ResultsPanel tab={$activeTab} />
        </div>
      </div>
    </div>

    <!-- AI Panel resize handle -->
    {#if $ai.visible}
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div
        class="w-1 flex-shrink-0 cursor-col-resize hover:bg-accent transition-colors {dragging === 'ai' ? 'bg-accent' : 'bg-border'}"
        on:mousedown={onAiHandleDown}
      ></div>

      <div
        class="flex-shrink-0 border-l border-border overflow-hidden"
        style="width: {aiPanelWidth}px"
      >
        <AiPanel />
      </div>
    {/if}
  </div>
</div>
