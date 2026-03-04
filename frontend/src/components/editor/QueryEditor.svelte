<script lang="ts">
  import { onMount, onDestroy } from "svelte";
  import { get } from "svelte/store";
  import { activeTab, updateTabSQL, editor } from "../../lib/stores/editor";
  import { schema } from "../../lib/stores/schema";
  import type * as Monaco from "monaco-editor";

  let container: HTMLDivElement;
  let monacoEditor: Monaco.editor.IStandaloneCodeEditor | undefined;
  let monaco: typeof Monaco | undefined;
  let completionDisposable: Monaco.IDisposable | undefined;

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

    // Schema-aware autocomplete
    const m = monaco;
    completionDisposable = m.languages.registerCompletionItemProvider("sql", {
      triggerCharacters: [".", " ", "("],
      provideCompletionItems(model, position) {
        const word = model.getWordUntilPosition(position);
        const range = {
          startLineNumber: position.lineNumber,
          endLineNumber: position.lineNumber,
          startColumn: word.startColumn,
          endColumn: word.endColumn,
        };

        const lineContent = model.getLineContent(position.lineNumber);
        const textBefore = lineContent.substring(0, position.column - 1);
        const dotMatch = textBefore.match(/(\w+)\.$/);

        const suggestions: Monaco.languages.CompletionItem[] = [];
        const s = get(schema);

        if (dotMatch) {
          const tableName = dotMatch[1];
          const cols = s.tableColumns[tableName] || [];
          for (const col of cols) {
            suggestions.push({
              label: col.name,
              kind: m.languages.CompletionItemKind.Field,
              detail: col.type + (col.is_primary ? " PK" : ""),
              insertText: col.name,
              range,
            });
          }
        } else {
          for (const t of s.tables) {
            suggestions.push({
              label: t.name,
              kind: m.languages.CompletionItemKind.Struct,
              detail: `${t.type}${t.row_count ? ` (${t.row_count} rows)` : ""}`,
              insertText: t.name,
              range,
            });
          }
          for (const db of s.databases) {
            suggestions.push({
              label: db,
              kind: m.languages.CompletionItemKind.Module,
              detail: "database",
              insertText: db,
              range,
            });
          }
        }
        return { suggestions };
      },
    });

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
      } else {
        // Same tab but SQL changed externally (e.g. Format button, history click)
        setMonacoValue(tab.sql);
      }
    });
  });

  onDestroy(() => {
    completionDisposable?.dispose();
    monacoEditor?.dispose();
    unsubActiveTab?.();
  });
</script>

<div class="h-full w-full" bind:this={container}></div>
