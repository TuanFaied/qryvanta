import { beforeEach, describe, expect, it, vi } from "vitest";
import { backend, type Analysis } from "@/services/backend";
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
  useAnalysisStore.setState({ target: null, running: false, result: null, error: null });
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
