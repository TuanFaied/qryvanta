import { beforeEach, describe, expect, it } from "vitest";
import { Outcome, type ExecutionEvent, type Summary } from "@/services/backend";
import { useExecutionStore } from "./execution-store";

const s = () => useExecutionStore.getState();

function event(runId: string, e: Partial<ExecutionEvent>): ExecutionEvent {
  return { runId, kind: "rows", index: 0, columns: [], rows: [], message: null, summary: null, ...e } as ExecutionEvent;
}

function summary(outcome: Outcome, extra: Partial<Summary> = {}): Summary {
  return {
    outcome,
    resultSets: [],
    rowsAffected: [],
    messages: [],
    errorCount: 0,
    truncatedBy: "",
    totalRows: 0,
    totalBytes: 0,
    elapsedMs: 12,
    guard: { active: true, rolledBack: true, endedByBatch: false },
    error: "",
    ...extra,
  };
}

const start = (runId: string, tabId = "t1") => s().start({ runId, tabId, connectionId: "c1", database: "Sales", lineOffset: 4 });

beforeEach(() => useExecutionStore.setState({ runs: {} }));

describe("execution store", () => {
  it("streams result sets, row batches and messages into the tab's run", () => {
    start("r1");
    s().apply(event("r1", { kind: "resultSet", index: 0, columns: [{ name: "id", type: "INT", nullable: false }] }));
    s().apply(event("r1", { kind: "rows", index: 0, rows: [[1], [2]] }));
    s().apply(event("r1", { kind: "rows", index: 0, rows: [[3]] }));
    s().apply(event("r1", { kind: "message", message: { kind: "info", number: 0, severity: 0, state: 0, line: 0, procedure: "", text: "hi", afterResultSet: 1 } }));
    s().apply(event("r1", { kind: "resultSet", index: 1, columns: [{ name: "x", type: "NVARCHAR", nullable: true }] }));
    s().apply(event("r1", { kind: "rows", index: 1, rows: [[null]] }));

    const run = s().runs.t1!;
    expect(run.status).toBe("running");
    expect(run.lineOffset).toBe(4);
    expect(run.resultSets.map((rs) => rs.rows)).toEqual([[[1], [2], [3]], [[null]]]);
    expect(run.messages.map((m) => m.text)).toEqual(["hi"]);

    s().apply(event("r1", { kind: "done", summary: summary(Outcome.OutcomeSucceeded, { totalRows: 4 }) }));
    expect(s().runs.t1).toMatchObject({ status: "finished", summary: { totalRows: 4 } });
    expect(s().runs.t1!.finishedAt).not.toBeNull();
  });

  it("drops events of a run that was replaced, so it cannot overwrite the newer one", () => {
    start("old");
    s().apply(event("old", { kind: "resultSet", index: 0, columns: [] }));
    start("new");
    s().apply(event("old", { kind: "rows", index: 0, rows: [["stale"]] }));
    s().apply(event("old", { kind: "done", summary: summary(Outcome.OutcomeCancelled) }));
    s().apply(event("unknown", { kind: "resultSet", index: 0, columns: [] }));

    expect(s().runs.t1).toMatchObject({ runId: "new", status: "running", resultSets: [] });
    s().finish("old", summary(Outcome.OutcomeSucceeded));
    expect(s().runs.t1!.status).toBe("running");
  });

  it("keeps runs of different tabs apart", () => {
    start("a", "t1");
    start("b", "t2");
    s().apply(event("b", { kind: "resultSet", index: 0, columns: [] }));
    expect(s().runs.t1!.resultSets).toHaveLength(0);
    expect(s().runs.t2!.resultSets).toHaveLength(1);
  });

  it("finishes once: the done event and the call's result do not overwrite each other", () => {
    start("r1");
    s().apply(event("r1", { kind: "done", summary: summary(Outcome.OutcomeCancelled) }));
    const finishedAt = s().runs.t1!.finishedAt;
    s().finish("r1", summary(Outcome.OutcomeSucceeded));
    expect(s().runs.t1!.summary?.outcome).toBe(Outcome.OutcomeCancelled);
    expect(s().runs.t1!.finishedAt).toBe(finishedAt);
  });

  it("accepts rows that arrive after the run finished, and records transport failures", () => {
    start("r1");
    s().apply(event("r1", { kind: "resultSet", index: 0, columns: [] }));
    s().finish("r1", summary(Outcome.OutcomeSucceeded));
    s().apply(event("r1", { kind: "rows", index: 0, rows: [[1]] }));
    expect(s().runs.t1!.resultSets[0]!.rows).toEqual([[1]]);

    start("r2");
    s().fail("r2", "Execute: connection lost");
    expect(s().runs.t1).toMatchObject({ status: "finished", error: "Execute: connection lost", summary: null });

    s().clear("t1");
    expect(s().runs.t1).toBeUndefined();
  });
});
