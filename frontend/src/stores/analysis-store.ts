import { create } from "zustand";
import { backend, type Analysis, type DebugPlan, type DiagnosisEvent, type Dismissal, type ProbeResult } from "@/services/backend";

/** The steps of Execute with debugging, in order. */
export const debugSteps = ["parse", "static", "metadata", "plan", "probes", "runProbes"] as const;
export type DebugStep = (typeof debugSteps)[number];

/** Where a step of Execute with debugging is: "pending" until its first event. */
export interface StepState {
  step: DebugStep;
  status: "pending" | "running" | "done" | "skipped" | "failed" | "waiting";
  reason: string;
}

const pendingSteps = (): StepState[] => debugSteps.map((step) => ({ step, status: "pending", reason: "" }));

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
  /** The step statuses of Execute with debugging; null for a plain Analyze. */
  steps: StepState[] | null;
  /** Runs Execute with debugging's flow on target; a newer run cancels the older one. Without
   * plan, no plan is read. */
  diagnose: (target: AnalysisTarget, plan?: DebugPlan) => Promise<void>;
  /** Applies a step status of the current diagnosis (events of other runs are dropped). */
  applyStep: (event: DiagnosisEvent) => void;
  /** Stops the running diagnosis (its finished steps and findings stay). */
  cancelDiagnosis: () => void;
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
let diagnosisRun = "";
let runCounter = 0;

export const useAnalysisStore = create<AnalysisStoreState>((set, get) => ({
  target: null,
  running: false,
  result: null,
  error: null,
  probes: noProbes,
  steps: null,

  analyze: async (target) => {
    inFlight?.abort();
    probesInFlight?.abort();
    diagnosisRun = "";
    const controller = new AbortController();
    inFlight = controller;
    set({ target, running: true, result: null, error: null, probes: noProbes, steps: null });
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

  diagnose: async (target, plan = { mode: "" }) => {
    inFlight?.abort();
    probesInFlight?.abort();
    const controller = new AbortController();
    inFlight = controller;
    const runId = `debug-${Date.now()}-${++runCounter}`;
    diagnosisRun = runId;
    set({ target, running: true, result: null, error: null, probes: noProbes, steps: pendingSteps() });
    try {
      const result = await backend.diagnose(runId, target.sql, target.connectionId, target.database, plan, controller.signal);
      if (diagnosisRun !== runId) return;
      set({ result, running: false, steps: mergeSteps(get().steps, result.steps ?? []) });
    } catch (err) {
      if (diagnosisRun !== runId) return;
      const stopped = controller.signal.aborted;
      set({
        running: false,
        error: stopped ? null : err instanceof Error ? err.message : String(err),
        steps: (get().steps ?? []).map((s) =>
          s.status === "pending" || s.status === "running"
            ? { ...s, status: "skipped", reason: stopped ? "Cancelled." : "Not reached." }
            : s,
        ),
      });
    } finally {
      if (inFlight === controller) inFlight = null;
    }
  },

  applyStep: (event) => {
    if (event.runId !== diagnosisRun) return;
    const steps = get().steps;
    if (!steps) return;
    set({
      steps: steps.map((s) => (s.step === event.step ? { ...s, status: event.status as StepState["status"], reason: event.reason } : s)),
    });
  },

  cancelDiagnosis: () => {
    if (diagnosisRun && get().running) inFlight?.abort();
  },

  forget: (tabId) => {
    if (get().target?.tabId !== tabId) return;
    inFlight?.abort();
    inFlight = null;
    probesInFlight?.abort();
    probesInFlight = null;
    diagnosisRun = "";
    set({ target: null, running: false, result: null, error: null, probes: noProbes, steps: null });
  },

  runProbes: async (probeIds) => {
    const analysis = get().result;
    if (!analysis || probeIds.length === 0 || get().probes.running) return;
    const controller = new AbortController();
    probesInFlight = controller;
    const setRunStep = (status: StepState["status"], reason: string) =>
      set((s) => ({ steps: s.steps?.map((st) => (st.step === "runProbes" ? { ...st, status, reason } : st)) ?? null }));
    set({ probes: { ...noProbes, running: true } });
    setRunStep("running", "");
    try {
      const run = await backend.runProbes(analysis.id, probeIds, controller.signal);
      if (controller.signal.aborted || get().result?.id !== analysis.id) return;
      if (run.error) {
        set({ probes: { ...noProbes, error: run.error } });
        setRunStep("failed", run.error);
        return;
      }
      set({
        result: { ...analysis, findings: run.findings },
        probes: { running: false, results: run.results ?? [], dismissed: run.dismissed ?? [], error: null },
      });
      setRunStep("done", `${(run.results ?? []).length} probe(s) ran; ${(run.dismissed ?? []).length} finding(s) dismissed.`);
    } catch (err) {
      if (get().result?.id !== analysis.id) return;
      set({
        probes: {
          ...noProbes,
          error: controller.signal.aborted ? "Cancelled: no findings were changed." : err instanceof Error ? err.message : String(err),
        },
      });
      setRunStep(controller.signal.aborted ? "skipped" : "failed", controller.signal.aborted ? "Cancelled." : "The probes could not run.");
    } finally {
      if (probesInFlight === controller) probesInFlight = null;
    }
  },

  cancelProbes: () => {
    probesInFlight?.abort();
  },
}));

/** The final step statuses of a diagnosis win over the events; steps it does not report stay. */
function mergeSteps(current: StepState[] | null, final: { step: string; status: string; reason: string }[]): StepState[] {
  const base = current ?? pendingSteps();
  return base.map((s) => {
    const f = final.find((x) => x.step === s.step);
    return f ? { ...s, status: f.status as StepState["status"], reason: f.reason } : s;
  });
}

/** Editor position of a 1-based line and column of the analyzed SQL. */
export function editorPosition(target: AnalysisTarget, line: number, column: number): { line: number; column: number } {
  return { line: line + target.startLine - 1, column: line === 1 ? column + target.startColumn - 1 : column };
}
