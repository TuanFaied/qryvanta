import { backend } from "@/services/backend";
import { useExecutionStore } from "@/stores/execution-store";

export interface RunRequest {
  tabId: string;
  connectionId: string;
  database: string | null;
  sql: string;
  /** Editor line where sql starts (1-based), to map server line numbers back to the editor. */
  startLine: number;
}

function newRunId(): string {
  if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") return crypto.randomUUID();
  return `run-${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
}

/**
 * Starts a run for a tab. A run still in progress for the same tab is cancelled first; its
 * late events are ignored because they carry the old run ID.
 */
export function executeQuery(req: RunRequest): string {
  const store = useExecutionStore.getState();
  const previous = store.runs[req.tabId];
  if (previous?.status === "running") cancelRun(previous.runId);

  const runId = newRunId();
  store.start({
    runId,
    tabId: req.tabId,
    connectionId: req.connectionId,
    database: req.database ?? "",
    lineOffset: Math.max(0, req.startLine - 1),
  });
  backend
    .execute(runId, req.connectionId, req.database ?? "", req.sql)
    // The "done" event normally finishes the run first; this covers a lost event.
    .then((summary) => useExecutionStore.getState().finish(runId, summary))
    .catch((err: unknown) => useExecutionStore.getState().fail(runId, err instanceof Error ? err.message : String(err)));
  return runId;
}

/** Asks the server to stop a run. The run ends when its summary arrives (outcome "cancelled"). */
export function cancelRun(runId: string): void {
  backend.cancelRun(runId).catch(() => {});
}

/** Routes streamed results into the store. Call once; returns an unsubscribe function. */
export function subscribeToExecutionEvents(): () => void {
  return backend.onExecutionEvent((event) => useExecutionStore.getState().apply(event));
}
