import { create } from "zustand";
import { backend, type Analysis, type Dismissal, type ProbeResult } from "@/services/backend";

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
  /** Connection and database whose catalog the metadata rules read ("" for none). */
  connectionId: string;
  database: string;
}

/** The probe run of the current analysis. */
export interface ProbeState {
  running: boolean;
  results: ProbeResult[];
  /** Findings the probes showed to be no problem, with the reason. */
  dismissed: Dismissal[];
  /** The probes could not run at all, or were cancelled. */
  error: string | null;
}

const noProbes: ProbeState = { running: false, results: [], dismissed: [], error: null };

interface AnalysisStoreState {
  target: AnalysisTarget | null;
  running: boolean;
  result: Analysis | null;
  /** The backend could not be reached. */
  error: string | null;
  probes: ProbeState;
  /** Runs the approved probes of the current analysis; their results update its findings. */
  runProbes: (probeIds: string[]) => Promise<void>;
  /** Cancels the running probes. */
  cancelProbes: () => void;
  /** Analyzes target; a newer analysis cancels the older one. */
  analyze: (target: AnalysisTarget) => Promise<void>;
  /** Forgets the analysis of a tab (for example when it closes). */
  forget: (tabId: string) => void;
}

let inFlight: AbortController | null = null;
let probesInFlight: AbortController | null = null;

export const useAnalysisStore = create<AnalysisStoreState>((set, get) => ({
  target: null,
  running: false,
  result: null,
  error: null,
  probes: noProbes,

  analyze: async (target) => {
    inFlight?.abort();
    probesInFlight?.abort();
    const controller = new AbortController();
    inFlight = controller;
    set({ target, running: true, result: null, error: null, probes: noProbes });
    try {
      const result = await backend.analyze(target.sql, target.connectionId, target.database, controller.signal);
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
    probesInFlight?.abort();
    probesInFlight = null;
    set({ target: null, running: false, result: null, error: null, probes: noProbes });
  },

  runProbes: async (probeIds) => {
    const analysis = get().result;
    if (!analysis || probeIds.length === 0 || get().probes.running) return;
    const controller = new AbortController();
    probesInFlight = controller;
    set({ probes: { ...noProbes, running: true } });
    try {
      const run = await backend.runProbes(analysis.id, probeIds, controller.signal);
      if (controller.signal.aborted || get().result?.id !== analysis.id) return;
      if (run.error) {
        set({ probes: { ...noProbes, error: run.error } });
        return;
      }
      set({
        result: { ...analysis, findings: run.findings },
        probes: { running: false, results: run.results ?? [], dismissed: run.dismissed ?? [], error: null },
      });
    } catch (err) {
      if (get().result?.id !== analysis.id) return;
      set({
        probes: {
          ...noProbes,
          error: controller.signal.aborted ? "Cancelled: no findings were changed." : err instanceof Error ? err.message : String(err),
        },
      });
    } finally {
      if (probesInFlight === controller) probesInFlight = null;
    }
  },

  cancelProbes: () => {
    probesInFlight?.abort();
  },
}));

/** Editor position of a 1-based line and column of the analyzed SQL. */
export function editorPosition(target: AnalysisTarget, line: number, column: number): { line: number; column: number } {
  return { line: line + target.startLine - 1, column: line === 1 ? column + target.startColumn - 1 : column };
}
