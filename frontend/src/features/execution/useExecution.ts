import { useCallback, useEffect, useMemo, useRef, useState, type RefObject } from "react";
import type { SqlEditorHandle } from "@/features/editor/editor-types";
import { AccessLevel, type Config, type WriteAccess } from "@/services/backend";
import { accessKey, useAccessStore } from "@/stores/access-store";
import { useActiveTab } from "@/stores/editor-store";
import { useExecutionStore, type Run } from "@/stores/execution-store";
import { cancelRun, executeQuery, subscribeToExecutionEvents, type RunRequest } from "./run-query";

/** A run waiting for the user to confirm that data may be modified. */
export interface PendingRun {
  request: RunRequest;
  access: WriteAccess | null;
}

interface Options {
  config: Config;
  /** The connection the workbench has open; a tab can only run on it. */
  activeConnectionId: string | null;
  editorRef: RefObject<SqlEditorHandle | null>;
  onStarted: () => void;
  onFinished: (run: Run) => void;
}

// Connections the user agreed to run on with read-only mode off, for this app session.
const confirmedWriteConnections = new Set<string>();

/** Test hook: forget confirmations. */
export function resetWriteConfirmations(): void {
  confirmedWriteConnections.clear();
}

/**
 * Execute and Stop for the active editor tab. With read-only mode off, the first run on a
 * connection whose login may modify data (or whose permissions could not be checked) asks
 * for confirmation, because changes will be committed.
 */
export function useExecution({ config, activeConnectionId, editorRef, onStarted, onFinished }: Options) {
  const tab = useActiveTab();
  const runs = useExecutionStore((s) => s.runs);
  const run = tab ? (runs[tab.id] ?? null) : null;
  const running = run?.status === "running";
  const runningTabs = useMemo(
    () => new Set(Object.values(runs).filter((r) => r.status === "running").map((r) => r.tabId)),
    [runs],
  );
  const [pending, setPending] = useState<PendingRun | null>(null);

  useEffect(() => subscribeToExecutionEvents(), []);

  // Report each run once when it finishes, so the workbench can pick the output tab.
  const reported = useRef(new Set<string>());
  const finishedRef = useRef(onFinished);
  finishedRef.current = onFinished;
  useEffect(() => {
    if (run?.status === "finished" && !reported.current.has(run.runId)) {
      reported.current.add(run.runId);
      finishedRef.current(run);
    }
  }, [run]);

  let blockedReason: string | null = null;
  if (!tab) blockedReason = "open a query tab first";
  else if (!tab.connectionId || tab.connectionId !== activeConnectionId) blockedReason = "connect to a server first";
  else if (running) blockedReason = "a query is already running";

  const start = useCallback(
    (request: RunRequest) => {
      executeQuery(request);
      onStarted();
    },
    [onStarted],
  );

  const execute = useCallback(() => {
    if (blockedReason || !tab?.connectionId) return;
    const target = editorRef.current?.getRunTarget();
    if (!target || target.sql.trim() === "") return;
    const request: RunRequest = {
      tabId: tab.id,
      connectionId: tab.connectionId,
      database: tab.database,
      sql: target.sql,
      startLine: target.startLine,
    };
    if (!config.execution.readOnly && !confirmedWriteConnections.has(tab.connectionId)) {
      const access = tab.database ? (useAccessStore.getState().results[accessKey(tab.connectionId, tab.database)] ?? null) : null;
      if (access?.level !== AccessLevel.AccessReadOnly) {
        setPending({ request, access });
        return;
      }
    }
    start(request);
  }, [blockedReason, tab, editorRef, config.execution.readOnly, start]);

  const stop = useCallback(() => {
    if (run?.status === "running") cancelRun(run.runId);
  }, [run]);

  const confirmPending = useCallback(() => {
    if (!pending) return;
    confirmedWriteConnections.add(pending.request.connectionId);
    setPending(null);
    start(pending.request);
  }, [pending, start]);

  return {
    run,
    running,
    runningTabs,
    blockedReason,
    execute,
    stop,
    pending,
    confirmPending,
    cancelPending: () => setPending(null),
  };
}
