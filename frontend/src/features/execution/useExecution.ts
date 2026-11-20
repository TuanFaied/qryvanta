import { useCallback, useEffect, useMemo, useRef, useState, type RefObject } from "react";
import type { SqlEditorHandle } from "@/features/editor/editor-types";
import { AccessLevel, type Config, type Diagnostic, type WriteAccess } from "@/services/backend";
import { accessKey, useAccessStore } from "@/stores/access-store";
import { useActiveTab } from "@/stores/editor-store";
import { useExecutionStore, type Run } from "@/stores/execution-store";
import { currentDiagnostics, diagnosticsInLines, useSyntaxStore } from "@/stores/syntax-store";
import { cancelRun, executeQuery, subscribeToExecutionEvents, type RunRequest } from "./run-query";

/** A run waiting for the user to confirm that data may be modified. */
export interface PendingRun {
  request: RunRequest;
  access: WriteAccess | null;
}

/** A run waiting for the user to confirm running SQL with syntax errors. */
export interface PendingSyntax {
  request: RunRequest;
  database: string | null;
  diagnostics: Diagnostic[];
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
  const [pendingSyntax, setPendingSyntax] = useState<PendingSyntax | null>(null);

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

  // The write-access confirmation, then the run.
  const proceed = useCallback(
    (request: RunRequest, database: string | null) => {
      if (!config.execution.readOnly && !confirmedWriteConnections.has(request.connectionId)) {
        const access = database ? (useAccessStore.getState().results[accessKey(request.connectionId, database)] ?? null) : null;
        if (access?.level !== AccessLevel.AccessReadOnly) {
          setPending({ request, access });
          return;
        }
      }
      start(request);
    },
    [config.execution.readOnly, start],
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
    // Syntax errors found by the editor's check (only when it is current for this text, and
    // only those inside the selection when a selection runs) are confirmed first.
    const all = currentDiagnostics(useSyntaxStore.getState().byTab[tab.id], tab.content);
    const lastLine = target.startLine + target.sql.split("\n").length - 1;
    const errors = target.fromSelection ? diagnosticsInLines(all, target.startLine, lastLine) : all;
    if (errors.length > 0) {
      setPendingSyntax({ request, database: tab.database, diagnostics: errors });
      return;
    }
    proceed(request, tab.database);
  }, [blockedReason, tab, editorRef, proceed]);

  const stop = useCallback(() => {
    if (run?.status === "running") cancelRun(run.runId);
  }, [run]);

  const confirmPending = useCallback(() => {
    if (!pending) return;
    confirmedWriteConnections.add(pending.request.connectionId);
    setPending(null);
    start(pending.request);
  }, [pending, start]);

  const confirmSyntax = useCallback(() => {
    if (!pendingSyntax) return;
    setPendingSyntax(null);
    proceed(pendingSyntax.request, pendingSyntax.database);
  }, [pendingSyntax, proceed]);

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
    pendingSyntax,
    confirmSyntax,
    cancelSyntax: () => setPendingSyntax(null),
  };
}
