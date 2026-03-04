<script lang="ts">
  import { onMount, onDestroy } from "svelte";
  import { activeTab, updateTabSQL, editor } from "../../lib/stores/editor";
  import type * as Monaco from "monaco-editor";

  let container: HTMLDivElement;
  let monacoEditor: Monaco.editor.IStandaloneCodeEditor | undefined;
  let monaco: typeof Monaco | undefined;

  // Track the active tab to update editor content
  let currentTabId = "";
  let suppressContentChange = false;
  let unsubActiveTab: (() => void) | undefined;

  // Helper to set Monaco content without triggering the change listener loop
  function setMonacoValue(sql: string) {
    if (!monacoEditor) return;
    const model = monacoEditor.getModel();
    if (!model) return;
    // Only update if content actually differs
    if (model.getValue() === sql) return;
    suppressContentChange = true;
    model.setValue(sql);
    suppressContentChange = false;
  }

  onMount(async () => {
    // Dynamic import of Monaco — keeps initial bundle small
    monaco = await import("monaco-editor");

    // Configure SQL language defaults
    monacoEditor = monaco.editor.create(container, {
      value: $activeTab?.sql ?? "",
      language: "sql",
      theme: "vs-dark",
      minimap: { enabled: false },
      fontSize: 13,
      fontFamily: "'JetBrains Mono', 'Fira Code', 'Menlo', monospace",
      lineNumbers: "on",
      lineHeight: 20,
      padding: { top: 8, bottom: 8 },
      scrollBeyondLastLine: false,
      wordWrap: "on",
      automaticLayout: true,
      tabSize: 2,
      renderWhitespace: "none",
      overviewRulerBorder: false,
      hideCursorInOverviewRuler: true,
      scrollbar: {
        verticalScrollbarSize: 6,
        horizontalScrollbarSize: 6,
      },
      suggestOnTriggerCharacters: true,
    });

    // Customize the dark theme colors
    monaco.editor.defineTheme("warehouse-dark", {
      base: "vs-dark",
      inherit: true,
      rules: [],
      colors: {
        "editor.background": "#0f0f14",
        "editor.foreground": "#e4e4ef",
        "editor.lineHighlightBackground": "#1a1a2400",
        "editor.selectionBackground": "#4a6cf740",
        "editorGutter.background": "#0f0f14",
        "editorLineNumber.foreground": "#5a5a70",
        "editorLineNumber.activeForeground": "#8888a0",
      },
    });
    monaco.editor.setTheme("warehouse-dark");

    // Listen for content changes from user typing
    monacoEditor.onDidChangeModelContent(() => {
      if (suppressContentChange) return;
      const tab = $activeTab;
      if (tab && monacoEditor) {
        updateTabSQL(tab.id, monacoEditor.getValue());
      }
    });

    // Ctrl/Cmd+Enter to run query
    monacoEditor.addAction({
      id: "run-query",
      label: "Run Query",
      keybindings: [
        monaco.KeyMod.CtrlCmd | monaco.KeyCode.Enter,
      ],
      run: () => {
        // Dispatch a custom event that the Toolbar listens for
        container.dispatchEvent(
          new CustomEvent("run-query", { bubbles: true })
        );
      },
    });

    currentTabId = $activeTab?.id ?? "";

    // Explicit store subscription for tab switching — more reliable than $: reactive block
    // with async-initialized monacoEditor
    unsubActiveTab = activeTab.subscribe((tab) => {
      if (!monacoEditor || !tab) return;
      if (tab.id !== currentTabId) {
        currentTabId = tab.id;
        setMonacoValue(tab.sql);
      }
    });
  });

  onDestroy(() => {
    monacoEditor?.dispose();
    unsubActiveTab?.();
  });
</script>

<div class="h-full w-full" bind:this={container}></div>
