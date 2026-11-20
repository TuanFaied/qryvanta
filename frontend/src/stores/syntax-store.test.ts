import { beforeEach, describe, expect, it, vi } from "vitest";
import { backend, type SyntaxCheck } from "@/services/backend";
import { currentDiagnostics, diagnosticsInLines, useSyntaxStore } from "./syntax-store";

const s = () => useSyntaxStore.getState();
const diag = (line: number) => ({ number: 102, message: "Incorrect syntax.", offset: 0, byteOffset: 0, line, column: 1 });
const ok = (diagnostics = [diag(1)]): SyntaxCheck => ({ available: true, reason: "", batches: 1, diagnostics });

beforeEach(() => {
  vi.restoreAllMocks();
  useSyntaxStore.setState({ byTab: {} });
});

describe("syntax store", () => {
  it("stores the result for the text that was checked", async () => {
    vi.spyOn(backend, "checkSyntax").mockResolvedValue(ok());
    await s().check("t1", "SELECT FROM");
    const state = s().byTab.t1;
    expect(currentDiagnostics(state, "SELECT FROM")).toHaveLength(1);
    // Once the text changed, the old markers are not current.
    expect(currentDiagnostics(state, "SELECT 1 FROM t")).toEqual([]);
  });

  it("aborts an older check of the same tab, so it cannot overwrite the newer one", async () => {
    let resolveFirst!: (v: SyntaxCheck) => void;
    const signals: AbortSignal[] = [];
    vi.spyOn(backend, "checkSyntax")
      .mockImplementationOnce((_sql, signal) => {
        signals.push(signal!);
        return new Promise((r) => (resolveFirst = r));
      })
      .mockImplementationOnce((_sql, signal) => {
        signals.push(signal!);
        return Promise.resolve(ok([]));
      });
    const first = s().check("t1", "SELEC");
    await s().check("t1", "SELECT 1");
    expect(signals[0]!.aborted).toBe(true);
    resolveFirst(ok());
    await first;
    expect(s().byTab.t1).toMatchObject({ text: "SELECT 1", diagnostics: [] });
  });

  it("shows no markers when the parser is unavailable, and keeps the old state on failure", async () => {
    vi.spyOn(backend, "checkSyntax").mockResolvedValueOnce({ available: false, reason: "not found", batches: 0, diagnostics: [] });
    await s().check("t1", "SELECT FROM");
    expect(currentDiagnostics(s().byTab.t1, "SELECT FROM")).toEqual([]);

    vi.spyOn(backend, "checkSyntax").mockRejectedValueOnce(new Error("backend gone"));
    await s().check("t1", "SELECT 2");
    expect(s().byTab.t1?.text).toBe("SELECT FROM");
  });

  it("forgets closed tabs", async () => {
    vi.spyOn(backend, "checkSyntax").mockResolvedValue(ok());
    await s().check("t1", "x");
    s().forget("t1");
    expect(s().byTab.t1).toBeUndefined();
  });

  it("filters diagnostics to a selection's lines", () => {
    expect(diagnosticsInLines([diag(1), diag(4), diag(9)], 3, 5).map((d) => d.line)).toEqual([4]);
  });
});
