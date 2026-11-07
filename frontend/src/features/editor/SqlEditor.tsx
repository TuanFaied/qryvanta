import { useEffect, useImperativeHandle, useRef, type Ref } from "react";
import type { Formatting } from "@/services/backend";
import type { ResolvedTheme } from "@/features/theme/theme";
import { formatSql } from "./format";
import { registerSqlCompletion } from "./completion/provider";
import { monaco, SQL_LANGUAGE } from "./monaco";

registerSqlCompletion();
import type { EditorCommand, SqlEditorHandle } from "./editor-types";

export interface SqlEditorProps {
  /** The tab shown. Each tab has its own Monaco model, so undo history and the cursor survive tab switches. */
  tabId: string;
  /** Every open tab, so models of closed tabs are disposed. */
  tabIds: string[];
  /** The tab's content. Changes from outside the editor (opening a file) replace the model text. */
  value: string;
  theme: ResolvedTheme;
  fontSize: number;
  formatting: Formatting;
  /** True while a query runs; enables Esc as "cancel execution". */
  executing?: boolean;
  onChange: (tabId: string, value: string) => void;
  onCommand: (command: EditorCommand) => void;
  /** Reports a formatting failure, or null when formatting succeeded. */
  onFormatResult: (error: string | null) => void;
  handleRef?: Ref<SqlEditorHandle>;
}

interface TabModel {
  model: monaco.editor.ITextModel;
  viewState: monaco.editor.ICodeEditorViewState | null;
  /** Last text exchanged with the store, to tell external changes from our own. */
  synced: string;
}

/** The Monaco T-SQL editor. Loaded lazily (see monaco.ts); tests replace it with a mock. */
export default function SqlEditor(props: SqlEditorProps) {
  const { tabId, tabIds, value, theme, fontSize, formatting, executing = false, handleRef } = props;
  const hostRef = useRef<HTMLDivElement>(null);
  const editorRef = useRef<monaco.editor.IStandaloneCodeEditor | null>(null);
  const modelsRef = useRef(new Map<string, TabModel>());
  const currentTab = useRef<string | null>(null);
  const executingKey = useRef<monaco.editor.IContextKey<boolean> | null>(null);
  // Callbacks change every render; Monaco commands read them through a ref.
  const latest = useRef(props);
  latest.current = props;

  const format = () => {
    const editor = editorRef.current;
    const model = editor?.getModel();
    if (!editor || !model) return;
    const selection = editor.getSelection();
    const range = selection && !selection.isEmpty() ? selection : model.getFullModelRange();
    const result = formatSql(model.getValueInRange(range), latest.current.formatting);
    if (!result.ok) {
      latest.current.onFormatResult(result.error);
      return;
    }
    latest.current.onFormatResult(null);
    if (result.text === model.getValueInRange(range)) return;
    // An edit (not setValue) so Ctrl+Z undoes the formatting.
    editor.pushUndoStop();
    editor.executeEdits("qryvanta.format", [{ range, text: result.text, forceMoveMarkers: true }]);
    editor.pushUndoStop();
    editor.focus();
  };

  useImperativeHandle(handleRef, () => ({
    format,
    focus: () => editorRef.current?.focus(),
    getRunTarget: () => {
      const editor = editorRef.current;
      const model = editor?.getModel();
      if (!editor || !model) return null;
      const selection = editor.getSelection();
      if (selection && !selection.isEmpty()) {
        return { sql: model.getValueInRange(selection), startLine: selection.startLineNumber, fromSelection: true };
      }
      return { sql: model.getValue(), startLine: 1, fromSelection: false };
    },
    revealLine: (line: number) => {
      const editor = editorRef.current;
      const model = editor?.getModel();
      if (!editor || !model) return;
      const target = Math.min(Math.max(line, 1), model.getLineCount());
      const column = model.getLineFirstNonWhitespaceColumn(target) || 1;
      editor.setPosition({ lineNumber: target, column });
      editor.revealLineInCenterIfOutsideViewport(target);
      editor.focus();
    },
  }));

  // Create the editor once.
  useEffect(() => {
    const host = hostRef.current;
    if (!host) return;
    const editor = monaco.editor.create(host, {
      model: null,
      theme: theme === "dark" ? "qryvanta-dark" : "qryvanta-light",
      fontSize,
      fontFamily: '"JetBrains Mono", "Cascadia Code", "Cascadia Mono", Consolas, monospace',
      automaticLayout: true,
      minimap: { enabled: false },
      scrollBeyondLastLine: false,
      renderWhitespace: "selection",
      bracketPairColorization: { enabled: true },
      folding: true,
      lineNumbers: "on",
      wordWrap: "off",
      tabSize: formatting.indentSize,
      insertSpaces: !formatting.useTabs,
      fixedOverflowWidgets: true,
      contextmenu: true,
      // Suggestions come from the schema-aware provider; words already in the document add noise.
      wordBasedSuggestions: "off",
      quickSuggestions: { other: true, comments: false, strings: false },
      suggest: { showWords: false },
      ariaLabel: "SQL editor",
    });
    editorRef.current = editor;
    executingKey.current = editor.createContextKey<boolean>("qryvanta.executing", false);

    const { KeyMod, KeyCode } = monaco;
    editor.addAction({
      id: "qryvanta.formatDocument",
      label: "Format Document",
      keybindings: [KeyMod.Shift | KeyMod.Alt | KeyCode.KeyF],
      contextMenuGroupId: "1_modification",
      run: () => format(),
    });
    // Execution shortcuts are registered here so Monaco does not apply its own meaning
    // (Ctrl+Enter would insert a line). Execution itself is handled by the workbench.
    editor.addAction({
      id: "qryvanta.execute",
      label: "Execute",
      keybindings: [KeyMod.CtrlCmd | KeyCode.Enter],
      run: () => latest.current.onCommand("execute"),
    });
    editor.addAction({
      id: "qryvanta.executeDebug",
      label: "Execute with Debugging",
      keybindings: [KeyMod.CtrlCmd | KeyMod.Shift | KeyCode.Enter],
      run: () => latest.current.onCommand("executeDebug"),
    });
    // Esc cancels only while a query runs, so it still closes the find widget and suggestions.
    editor.addCommand(KeyCode.Escape, () => latest.current.onCommand("cancel"), "qryvanta.executing");

    const sub = editor.onDidChangeModelContent(() => {
      const id = currentTab.current;
      const entry = id ? modelsRef.current.get(id) : undefined;
      if (!id || !entry) return;
      const text = entry.model.getValue();
      entry.synced = text;
      latest.current.onChange(id, text);
    });

    const models = modelsRef.current;
    return () => {
      sub.dispose();
      editor.dispose();
      for (const entry of models.values()) entry.model.dispose();
      models.clear();
      editorRef.current = null;
      currentTab.current = null;
    };
    // The editor is created once; option changes are applied by the effects below.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Show the active tab's model, creating it on first use.
  useEffect(() => {
    const editor = editorRef.current;
    if (!editor) return;
    const models = modelsRef.current;
    const previous = currentTab.current;
    if (previous === tabId) return;
    if (previous) {
      const prevEntry = models.get(previous);
      if (prevEntry) prevEntry.viewState = editor.saveViewState();
    }
    let entry = models.get(tabId);
    if (!entry) {
      const model = monaco.editor.createModel(value, SQL_LANGUAGE, monaco.Uri.parse(`inmemory://qryvanta/${tabId}.sql`));
      entry = { model, viewState: null, synced: value };
      models.set(tabId, entry);
    }
    currentTab.current = tabId;
    editor.setModel(entry.model);
    if (entry.viewState) editor.restoreViewState(entry.viewState);
    editor.focus();
    // value is read only when the model is created; later changes go through the next effect.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tabId]);

  // Apply content changes that did not come from typing (a file opened into this tab).
  useEffect(() => {
    const entry = modelsRef.current.get(tabId);
    if (!entry || value === entry.synced) return;
    entry.synced = value;
    entry.model.pushEditOperations([], [{ range: entry.model.getFullModelRange(), text: value }], () => null);
  }, [tabId, value]);

  // Dispose models of closed tabs.
  const tabKey = tabIds.join("\n");
  useEffect(() => {
    const open = new Set(tabKey.split("\n"));
    for (const [id, entry] of modelsRef.current) {
      if (!open.has(id)) {
        entry.model.dispose();
        modelsRef.current.delete(id);
      }
    }
  }, [tabKey]);

  useEffect(() => {
    monaco.editor.setTheme(theme === "dark" ? "qryvanta-dark" : "qryvanta-light");
  }, [theme]);

  useEffect(() => {
    editorRef.current?.updateOptions({ fontSize });
  }, [fontSize]);

  useEffect(() => {
    for (const { model } of modelsRef.current.values()) {
      model.updateOptions({ tabSize: formatting.indentSize, insertSpaces: !formatting.useTabs });
    }
    editorRef.current?.updateOptions({ tabSize: formatting.indentSize, insertSpaces: !formatting.useTabs });
  }, [formatting.indentSize, formatting.useTabs, tabId]);

  useEffect(() => {
    executingKey.current?.set(executing);
  }, [executing]);

  return <div ref={hostRef} className="h-full w-full" data-testid="sql-editor" />;
}
