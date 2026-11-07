/** Commands the editor raises for the workbench to handle. */
export type EditorCommand = "execute" | "executeDebug" | "cancel";

/** Imperative access to the editor, used by the toolbar. */
export interface SqlEditorHandle {
  /** Formats the selection, or the whole document when nothing is selected. */
  format: () => void;
  focus: () => void;
}
