import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { Run } from "@/stores/execution-store";

vi.mock("./ResultGrid", () => ({ ROW_HEIGHT: 22, ResultGrid: ({ label }: { label: string }) => <div>{label} grid</div> }));
const { ResultsView } = await import("./ResultsView");

const batch = (b: number) => ({ batch: b, execution: 0, line: 1 + b * 2, errorCount: 0, elapsedMs: 1 });

function run(batches: number[]): Run {
  return {
    runId: "r1",
    tabId: "t1",
    connectionId: "c1",
    database: "Sales",
    status: "finished",
    startedAt: 0,
    finishedAt: 1,
    lineOffset: 0,
    batches: [...new Set(batches)].map(batch),
    resultSets: batches.map((b, i) => ({ index: i, batch: b, columns: [], rows: [[1]] })),
    messages: [],
    summary: null,
    error: null,
  };
}

describe("ResultsView", () => {
  it("names the batch of each result set when a script had several", () => {
    render(<ResultsView run={run([0, 1])} maxRows={100} />);
    expect(screen.getByRole("heading", { name: "Batch 1 · Result 1 · 1 row" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Batch 2 · Result 2 · 1 row" })).toBeInTheDocument();
  });

  it("leaves the batch out for a single batch", () => {
    render(<ResultsView run={run([0, 0])} maxRows={100} />);
    expect(screen.getByRole("heading", { name: "Result 2 · 1 row" })).toBeInTheDocument();
  });
});
