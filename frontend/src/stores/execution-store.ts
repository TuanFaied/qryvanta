import { create } from "zustand";
import type { ExecutionEvent, Message, ResultColumn, Summary, Value } from "@/services/backend";

export interface ResultSetData {
  index: number;
  columns: ResultColumn[];
  rows: Value[][];
}

/** One execution of an editor tab. Result rows live only here, in memory. */
export interface Run {
  runId: string;
  tabId: string;
  connectionId: string;
  database: string;
  status: "running" | "finished";
  startedAt: number;
  finishedAt: number | null;
  /** Editor line the batch started on, minus one: server line numbers are relative to the batch. */
  lineOffset: number;
  resultSets: ResultSetData[];
  messages: Message[];
  summary: Summary | null;
  /** A failure to reach the backend at all; server errors are in messages or summary.error. */
  error: string | null;
}

export interface StartRun {
  runId: string;
  tabId: string;
  connectionId: string;
  database: string;
  lineOffset: number;
}

interface ExecutionState {
  /** The latest run of each tab, by tab ID. */
  runs: Record<string, Run>;
  start: (run: StartRun) => void;
  /** Applies a streamed event. Events of runs that are not a tab's latest run are dropped. */
  apply: (event: ExecutionEvent) => void;
  finish: (runId: string, summary: Summary) => void;
  fail: (runId: string, error: string) => void;
  clear: (tabId: string) => void;
}

function update(runs: Record<string, Run>, runId: string, fn: (run: Run) => Run): Record<string, Run> {
  for (const [tabId, run] of Object.entries(runs)) {
    if (run.runId === runId) return { ...runs, [tabId]: fn(run) };
  }
  return runs;
}

function finished(run: Run, summary: Summary | null, error: string | null): Run {
  if (run.status === "finished") return run;
  return { ...run, status: "finished", finishedAt: Date.now(), summary, error };
}

export const useExecutionStore = create<ExecutionState>((set) => ({
  runs: {},

  start: (r) =>
    set((s) => ({
      runs: {
        ...s.runs,
        [r.tabId]: {
          ...r,
          status: "running",
          startedAt: Date.now(),
          finishedAt: null,
          resultSets: [],
          messages: [],
          summary: null,
          error: null,
        },
      },
    })),

  apply: (event) =>
    set((s) => {
      const runs = update(s.runs, event.runId, (run) => {
        switch (event.kind) {
          case "resultSet":
            return { ...run, resultSets: [...run.resultSets, { index: event.index, columns: event.columns ?? [], rows: [] }] };
          case "rows": {
            const incoming = event.rows ?? [];
            return {
              ...run,
              resultSets: run.resultSets.map((rs) => (rs.index === event.index ? { ...rs, rows: rs.rows.concat(incoming) } : rs)),
            };
          }
          case "message":
            return event.message ? { ...run, messages: [...run.messages, event.message] } : run;
          case "done":
            return finished(run, event.summary, null);
          default:
            return run;
        }
      });
      return runs === s.runs ? s : { runs };
    }),

  finish: (runId, summary) => set((s) => ({ runs: update(s.runs, runId, (run) => finished(run, summary, null)) })),

  fail: (runId, error) => set((s) => ({ runs: update(s.runs, runId, (run) => finished(run, null, error)) })),

  clear: (tabId) =>
    set((s) => {
      if (!(tabId in s.runs)) return s;
      const runs = { ...s.runs };
      delete runs[tabId];
      return { runs };
    }),
}));

export function isRunning(run: Run | undefined | null): boolean {
  return run?.status === "running";
}
