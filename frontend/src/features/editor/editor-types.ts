/** Commands the editor raises for the workbench to handle. */
export type EditorCommand = "execute" | "executeDebug" | "cancel";

/** What Execute runs: the selection if there is one, otherwise the whole document. */
export interface RunTarget {
  sql: string;
  /** Editor line (1-based) the SQL starts on. */
  startLine: number;
  /** Editor column (1-based) the SQL starts at on startLine; 1 unless a selection starts mid-line. */
  startColumn?: number;
  fromSelection: boolean;
}

/** Imperative access to the editor, used by the toolbar and the workbench. */
export interface SqlEditorHandle {
  /** Formats the selection, or the whole document when nothing is selected. */
  format: () => void;
  focus: () => void;
  getRunTarget: () => RunTarget | null;
  /** Moves the cursor to the start of a line and scrolls it into view. */
  revealLine: (line: number) => void;
  /** Selects length UTF-16 code units from line and column (1-based) and scrolls them into view. */
  selectRange: (line: number, column: number, length: number) => void;
}
