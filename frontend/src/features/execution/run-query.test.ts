import { beforeEach, describe, expect, it, vi } from "vitest";
import { Outcome } from "@/services/backend";
import { useExecutionStore } from "@/stores/execution-store";

const api = vi.hoisted(() => ({
  execute: vi.fn(),
  cancelRun: vi.fn(() => Promise.resolve(true)),
  handler: null as null | ((e: unknown) => void),
}));

vi.mock("@/services/backend", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/services/backend")>();
  return {
    ...original,
    backend: {
      ...original.backend,
      execute: api.execute,
      cancelRun: api.cancelRun,
      onExecutionEvent: (h: (e: unknown) => void) => {
        api.handler = h;
        return () => (api.handler = null);
      },
    },
  };
});

const { cancelRun, executeQuery, subscribeToExecutionEvents } = await import("./run-query");

const request = { tabId: "t1", connectionId: "c1", database: "Sales", sql: "SELECT 1", startLine: 3 };

beforeEach(() => {
  useExecutionStore.setState({ runs: {} });
  vi.clearAllMocks();
});

describe("run-query", () => {
  it("starts a run with a fresh ID on the tab's connection and database", async () => {
    api.execute.mockResolvedValue({ outcome: Outcome.OutcomeSucceeded });
    const id = executeQuery(request);
    expect(api.execute).toHaveBeenCalledWith(id, "c1", "Sales", "SELECT 1");
    expect(useExecutionStore.getState().runs.t1).toMatchObject({ runId: id, status: "running", lineOffset: 2 });
    await vi.waitFor(() => expect(useExecutionStore.getState().runs.t1!.status).toBe("finished"));
  });

  it("cancels the tab's previous run when a new one starts", () => {
    api.execute.mockReturnValue(new Promise(() => {}));
    const first = executeQuery(request);
    const second = executeQuery(request);
    expect(second).not.toBe(first);
    expect(api.cancelRun).toHaveBeenCalledWith(first);
    expect(useExecutionStore.getState().runs.t1!.runId).toBe(second);
  });

  it("records a failure to reach the backend", async () => {
    api.execute.mockRejectedValue(new Error("Execute: backend unavailable"));
    executeQuery(request);
    await vi.waitFor(() => expect(useExecutionStore.getState().runs.t1!.error).toBe("Execute: backend unavailable"));
  });

  it("cancels on the server and routes events into the store", () => {
    cancelRun("r9");
    expect(api.cancelRun).toHaveBeenCalledWith("r9");

    api.execute.mockReturnValue(new Promise(() => {}));
    const id = executeQuery(request);
    const unsubscribe = subscribeToExecutionEvents();
    api.handler?.({ runId: id, kind: "resultSet", index: 0, columns: [], rows: null, message: null, summary: null });
    expect(useExecutionStore.getState().runs.t1!.resultSets).toHaveLength(1);
    unsubscribe();
    expect(api.handler).toBeNull();
  });
});
