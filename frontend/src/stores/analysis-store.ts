import { create } from "zustand";
import { backend, type Analysis } from "@/services/backend";

/** What was analyzed: the selection or the whole tab, and where it starts in the editor. */
export interface AnalysisTarget {
  tabId: string;
  /** The analyzed SQL (finding spans are offsets into it). */
  sql: string;
  /** The tab's whole text when it was analyzed, to tell when the analysis is out of date. */
  tabText: string;
  /** Editor line (1-based) the SQL starts on. */
  startLine: number;
  /** Editor column (1-based) the SQL starts at on startLine. */
  startColumn: number;
}

interface AnalysisStoreState {
  target: AnalysisTarget | null;
  running: boolean;
  result: Analysis | null;
  /** The backend could not be reached. */
  error: string | null;
  /** Analyzes target; a newer analysis cancels the older one. */
  analyze: (target: AnalysisTarget) => Promise<void>;
  /** Forgets the analysis of a tab (for example when it closes). */
  forget: (tabId: string) => void;
}

let inFlight: AbortController | null = null;

export const useAnalysisStore = create<AnalysisStoreState>((set, get) => ({
  target: null,
  running: false,
  result: null,
  error: null,

  analyze: async (target) => {
    inFlight?.abort();
    const controller = new AbortController();
    inFlight = controller;
    set({ target, running: true, result: null, error: null });
    try {
      const result = await backend.analyze(target.sql, controller.signal);
      if (controller.signal.aborted) return;
      set({ result, running: false });
    } catch (err) {
      if (controller.signal.aborted) return;
      set({ running: false, error: err instanceof Error ? err.message : String(err) });
    } finally {
      if (inFlight === controller) inFlight = null;
    }
  },

  forget: (tabId) => {
    if (get().target?.tabId !== tabId) return;
    inFlight?.abort();
    inFlight = null;
    set({ target: null, running: false, result: null, error: null });
  },
}));

/** Editor position of a 1-based line and column of the analyzed SQL. */
export function editorPosition(target: AnalysisTarget, line: number, column: number): { line: number; column: number } {
  return { line: line + target.startLine - 1, column: line === 1 ? column + target.startColumn - 1 : column };
}
