import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { AccessLevel } from "@/services/backend";
import { accessKey, useAccessStore } from "@/stores/access-store";
import { useEditorStore } from "@/stores/editor-store";
import { useExecutionStore } from "@/stores/execution-store";
import { testConfig } from "@/test/fixtures";
import type { SqlEditorHandle } from "@/features/editor/editor-types";

const runner = vi.hoisted(() => ({ execute: vi.fn(() => "run-1"), cancel: vi.fn() }));
vi.mock("./run-query", () => ({
  executeQuery: runner.execute,
  cancelRun: runner.cancel,
  subscribeToExecutionEvents: () => () => {},
}));

const { useExecution, resetWriteConfirmations } = await import("./useExecution");

const handle: SqlEditorHandle = {
  format: () => {},
  focus: () => {},
  revealLine: () => {},
  getRunTarget: () => ({ sql: "UPDATE t SET x = 1", startLine: 5, fromSelection: true }),
};

function setup(readOnly: boolean, activeConnectionId: string | null = "c1") {
  const config = testConfig();
  config.execution.readOnly = readOnly;
  const onStarted = vi.fn();
  const hook = renderHook(() =>
    useExecution({ config, activeConnectionId, editorRef: { current: handle }, onStarted, onFinished: vi.fn() }),
  );
  return { hook, onStarted };
}

beforeEach(() => {
  vi.clearAllMocks();
  resetWriteConfirmations();
  useExecutionStore.setState({ runs: {} });
  useAccessStore.setState({ results: {}, loading: {} });
  useEditorStore.setState({ tabs: [], activeId: null, untitledCount: 0 });
  useEditorStore.getState().newTab({ connectionId: "c1", database: "Sales" });
});

describe("useExecution", () => {
  it("runs the selection on the tab's connection and database in read-only mode without asking", () => {
    const { hook, onStarted } = setup(true);
    expect(hook.result.current.blockedReason).toBeNull();
    act(() => hook.result.current.execute());
    expect(runner.execute).toHaveBeenCalledWith(
      expect.objectContaining({ connectionId: "c1", database: "Sales", sql: "UPDATE t SET x = 1", startLine: 5 }),
    );
    expect(onStarted).toHaveBeenCalled();
    expect(hook.result.current.pending).toBeNull();
  });

  it("is blocked without a connection or while the tab's query runs", () => {
    expect(setup(true, null).hook.result.current.blockedReason).toBe("connect to a server first");
    const tabId = useEditorStore.getState().activeId!;
    useExecutionStore.getState().start({ runId: "r", tabId, connectionId: "c1", database: "Sales", lineOffset: 0 });
    const { hook } = setup(true);
    expect(hook.result.current.blockedReason).toBe("a query is already running");
    expect(hook.result.current.running).toBe(true);
    act(() => hook.result.current.stop());
    expect(runner.cancel).toHaveBeenCalledWith("r");
  });

  it("asks once per connection when read-only mode is off and the login may write", () => {
    useAccessStore.setState({
      results: { [accessKey("c1", "Sales")]: { database: "Sales", level: AccessLevel.AccessCanWrite, reasons: [], error: "" } },
    });
    const { hook } = setup(false);
    act(() => hook.result.current.execute());
    expect(runner.execute).not.toHaveBeenCalled();
    expect(hook.result.current.pending?.access?.level).toBe(AccessLevel.AccessCanWrite);

    act(() => hook.result.current.cancelPending());
    expect(hook.result.current.pending).toBeNull();
    expect(runner.execute).not.toHaveBeenCalled();

    act(() => hook.result.current.execute());
    act(() => hook.result.current.confirmPending());
    expect(runner.execute).toHaveBeenCalledTimes(1);

    act(() => hook.result.current.execute());
    expect(runner.execute).toHaveBeenCalledTimes(2);
    expect(hook.result.current.pending).toBeNull();
  });

  it("asks when permissions are unknown, but not for a read-only login", () => {
    const { hook } = setup(false);
    act(() => hook.result.current.execute());
    expect(hook.result.current.pending).not.toBeNull();

    resetWriteConfirmations();
    useAccessStore.setState({
      results: { [accessKey("c1", "Sales")]: { database: "Sales", level: AccessLevel.AccessReadOnly, reasons: [], error: "" } },
    });
    const second = setup(false).hook;
    act(() => second.result.current.execute());
    expect(second.result.current.pending).toBeNull();
    expect(runner.execute).toHaveBeenCalledTimes(1);
  });
});
