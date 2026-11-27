import { beforeEach, describe, expect, it, vi } from "vitest";
import { backend, type Analysis, type DiagnosisEvent, type ProbeRun } from "@/services/backend";
import { editorPosition, useAnalysisStore, type AnalysisTarget } from "./analysis-store";

const s = () => useAnalysisStore.getState();
const target = (sql: string, extra: Partial<AnalysisTarget> = {}): AnalysisTarget => ({
  tabId: "t1",
  sql,
  tabText: sql,
  startLine: 1,
  startColumn: 1,
  connectionId: "",
  database: "",
  ...extra,
});
const result = (rules = 1): Analysis => ({
  id: "a1",
  probes: [],
  steps: [],
  stopped: false,
  available: true,
  reason: "",
  findings: [],
  failures: [],
  rules,
  statements: 1,
  syntaxErrors: 0,
  metadata: { checked: false, reason: "", unresolved: [] },
});

beforeEach(() => {
  vi.restoreAllMocks();
  useAnalysisStore.setState({ target: null, running: false, result: null, error: null, steps: null, probes: { running: false, results: [], dismissed: [], error: null } });
});

describe("analysis store", () => {
  it("stores the result with its target", async () => {
    vi.spyOn(backend, "analyze").mockResolvedValue(result());
    await s().analyze(target("SELECT 1"));
    expect(s().running).toBe(false);
    expect(s().result?.rules).toBe(1);
    expect(s().target?.sql).toBe("SELECT 1");
  });

  it("cancels an older analysis, so it cannot overwrite the newer one", async () => {
    let resolveFirst!: (v: Analysis) => void;
    const signals: AbortSignal[] = [];
    vi.spyOn(backend, "analyze")
      .mockImplementationOnce((_sql, _conn, _db, signal) => {
        signals.push(signal!);
        return new Promise((r) => (resolveFirst = r));
      })
      .mockImplementationOnce((_sql, _conn, _db, signal) => {
        signals.push(signal!);
        return Promise.resolve(result(2));
      });
    const first = s().analyze(target("SELEC"));
    await s().analyze(target("SELECT 2"));
    expect(signals[0]!.aborted).toBe(true);
    resolveFirst(result(1));
    await first;
    expect(s().result?.rules).toBe(2);
    expect(s().target?.sql).toBe("SELECT 2");
  });

  it("passes the tab's connection and database for the metadata rules", async () => {
    const analyze = vi.spyOn(backend, "analyze").mockResolvedValue(result());
    await s().analyze(target("SELECT 1", { connectionId: "c1", database: "Sales" }));
    expect(analyze).toHaveBeenCalledWith("SELECT 1", "c1", "Sales", expect.any(AbortSignal));
  });

  it("reports a backend error", async () => {
    vi.spyOn(backend, "analyze").mockRejectedValue(new Error("Analyze failed: no backend"));
    await s().analyze(target("SELECT 1"));
    expect(s().error).toBe("Analyze failed: no backend");
    expect(s().running).toBe(false);
  });

  it("forgets the analysis of a closed tab only", async () => {
    vi.spyOn(backend, "analyze").mockResolvedValue(result());
    await s().analyze(target("SELECT 1"));
    s().forget("other");
    expect(s().result).not.toBeNull();
    s().forget("t1");
    expect(s().result).toBeNull();
    expect(s().target).toBeNull();
  });
});

describe("editorPosition", () => {
  it("maps positions of a selection to the editor", () => {
    const t = target("a = NULL", { startLine: 3, startColumn: 9 });
    expect(editorPosition(t, 1, 1)).toEqual({ line: 3, column: 9 });
    expect(editorPosition(t, 2, 4)).toEqual({ line: 4, column: 4 });
    expect(editorPosition(target("x"), 1, 5)).toEqual({ line: 1, column: 5 });
  });
});

describe("probes", () => {
  const run = (extra: Partial<ProbeRun> = {}): ProbeRun => ({ error: "", results: [], findings: [], dismissed: [], ...extra });

  it("runs only the approved probes of the current analysis and replaces its findings", async () => {
    vi.spyOn(backend, "analyze").mockResolvedValue(result());
    await s().analyze(target("SELECT 1"));
    const runProbes = vi.spyOn(backend, "runProbes").mockResolvedValue(
      run({ results: [{ probeId: "p1", elapsedMs: 5, error: "", summary: "3 rows dropped.", showsProblem: true, values: {} }] }),
    );
    await s().runProbes(["p1"]);
    expect(runProbes).toHaveBeenCalledWith("a1", ["p1"], expect.any(AbortSignal));
    expect(s().probes.results).toHaveLength(1);
    expect(s().probes.running).toBe(false);
  });

  it("does nothing without approved probes or an analysis", async () => {
    const runProbes = vi.spyOn(backend, "runProbes");
    await s().runProbes(["p1"]);
    vi.spyOn(backend, "analyze").mockResolvedValue(result());
    await s().analyze(target("SELECT 1"));
    await s().runProbes([]);
    expect(runProbes).not.toHaveBeenCalled();
  });

  it("cancels the run and says so", async () => {
    vi.spyOn(backend, "analyze").mockResolvedValue(result());
    await s().analyze(target("SELECT 1"));
    vi.spyOn(backend, "runProbes").mockImplementation(
      (_id, _ids, signal) =>
        new Promise((_resolve, reject) => signal!.addEventListener("abort", () => reject(new Error("cancelled")))),
    );
    const pending = s().runProbes(["p1"]);
    expect(s().probes.running).toBe(true);
    s().cancelProbes();
    await pending;
    expect(s().probes.running).toBe(false);
    expect(s().probes.error).toBe("Cancelled: no findings were changed.");
  });

  it("reports a refusal from the backend", async () => {
    vi.spyOn(backend, "analyze").mockResolvedValue(result());
    await s().analyze(target("SELECT 1"));
    vi.spyOn(backend, "runProbes").mockResolvedValue(run({ error: "probe p3 is over the probe budget" }));
    await s().runProbes(["p3"]);
    expect(s().probes.error).toBe("probe p3 is over the probe budget");
  });
});

describe("Execute with debugging", () => {
  const steps = () => s().steps?.map((st) => `${st.step}:${st.status}`).join(" ");

  it("shows the steps as their events arrive, and the final statuses when it ends", async () => {
    let finish!: (a: Analysis) => void;
    let runId = "";
    vi.spyOn(backend, "diagnose").mockImplementation((id) => {
      runId = id;
      return new Promise((r) => (finish = r));
    });
    const pending = s().diagnose(target("SELECT 1"));
    expect(steps()).toBe("parse:pending static:pending metadata:pending plan:pending probes:pending runProbes:pending");
    s().applyStep({ runId, step: "parse", status: "done", reason: "1 batch(es).", findings: -1 } as unknown as DiagnosisEvent);
    s().applyStep({ runId: "an-older-run", step: "static", status: "failed", reason: "", findings: -1 } as unknown as DiagnosisEvent);
    s().applyStep({ runId, step: "static", status: "running", reason: "", findings: -1 } as unknown as DiagnosisEvent);
    expect(steps()).toBe("parse:done static:running metadata:pending plan:pending probes:pending runProbes:pending");
    expect(s().running).toBe(true);
    finish({
      ...result(),
      steps: [
        { step: "parse", status: "done", reason: "1 batch(es)." },
        { step: "static", status: "done", reason: "0 findings." },
        { step: "metadata", status: "skipped", reason: "Not connected." },
        { step: "plan", status: "skipped", reason: "Plans need a connection." },
        { step: "probes", status: "skipped", reason: "Probes need a connection." },
        { step: "runProbes", status: "skipped", reason: "No probes were proposed." },
      ] as unknown as Analysis["steps"],
    });
    await pending;
    expect(steps()).toBe("parse:done static:done metadata:skipped plan:skipped probes:skipped runProbes:skipped");
    expect(s().steps?.[2]?.reason).toBe("Not connected.");
    expect(s().running).toBe(false);
  });

  it("marks the steps not reached as cancelled when stopped part-way", async () => {
    let runId = "";
    vi.spyOn(backend, "diagnose").mockImplementation(
      (id, _sql, _conn, _db, _plan, signal) =>
        new Promise((_r, reject) => {
          runId = id;
          signal!.addEventListener("abort", () => reject(new Error("cancelled")));
        }),
    );
    const pending = s().diagnose(target("SELECT 1"));
    s().applyStep({ runId, step: "parse", status: "done", reason: "", findings: -1 } as unknown as DiagnosisEvent);
    s().applyStep({ runId, step: "static", status: "running", reason: "", findings: -1 } as unknown as DiagnosisEvent);
    s().cancelDiagnosis();
    await pending;
    expect(steps()).toBe("parse:done static:skipped metadata:skipped plan:skipped probes:skipped runProbes:skipped");
    expect(s().steps?.[1]?.reason).toBe("Cancelled.");
    expect(s().error).toBeNull();
  });

  it("passes the tab's connection and a fresh run ID", async () => {
    const diagnose = vi.spyOn(backend, "diagnose").mockResolvedValue(result());
    await s().diagnose(target("SELECT 1", { connectionId: "c1", database: "Sales" }));
    await s().diagnose(target("SELECT 1", { connectionId: "c1", database: "Sales" }));
    const [first, second] = diagnose.mock.calls;
    expect(first?.slice(1, 4)).toEqual(["SELECT 1", "c1", "Sales"]);
    expect(first?.[0]).not.toBe(second?.[0]);
    expect(first?.[4]).toEqual({ mode: "" });
  });

  it("passes the plan to read", async () => {
    const diagnose = vi.spyOn(backend, "diagnose").mockResolvedValue(result());
    await s().diagnose(target("SELECT 1", { connectionId: "c1" }), { mode: "actual", runId: "run-7" });
    expect(diagnose.mock.calls[0]?.[4]).toEqual({ mode: "actual", runId: "run-7" });
  });

  it("a plain Analyze has no steps", async () => {
    vi.spyOn(backend, "analyze").mockResolvedValue(result());
    await s().analyze(target("SELECT 1"));
    expect(s().steps).toBeNull();
  });
});
