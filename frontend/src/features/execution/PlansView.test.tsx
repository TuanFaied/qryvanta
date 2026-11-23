import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { backend, type PlanInfo } from "@/services/backend";
import type { Run } from "@/stores/execution-store";
import { PlansView } from "./PlansView";

const run = (extra: Partial<Run> = {}): Run =>
  ({
    runId: "r1",
    tabId: "t1",
    connectionId: "c1",
    database: "Sales",
    status: "finished",
    startedAt: 0,
    finishedAt: 10,
    lineOffset: 0,
    batches: [],
    resultSets: [],
    messages: [],
    plan: "estimated",
    plans: [],
    summary: null,
    error: null,
    ...extra,
  }) as Run;

afterEach(() => vi.restoreAllMocks());

describe("PlansView", () => {
  it("explains how to capture a plan", () => {
    render(<PlansView run={null} />);
    expect(screen.getByText(/No plan captured/)).toHaveTextContent("Show estimated plan (Ctrl+L)");
    expect(screen.getByText(/No plan captured/)).toHaveTextContent("Include actual plan (Ctrl+M)");
  });

  it("shows progress, and when the server returned no plans", () => {
    const { unmount } = render(<PlansView run={run({ status: "running" })} />);
    expect(screen.getByText("Compiling…")).toBeInTheDocument();
    unmount();
    const summary = { plan: { mode: "actual", count: 0, tooLarge: 1, complete: false, reason: "1 plan(s) were larger than the 32 MB plan limit." } } as never;
    render(<PlansView run={run({ plan: "actual", summary })} />);
    expect(screen.getByRole("status")).toHaveTextContent("No plans: the server returned none");
    expect(screen.getByRole("alert")).toHaveTextContent("Incomplete: 1 plan(s) were larger than the 32 MB plan limit.");
  });

  it("hands a run with plans to the plan view", async () => {
    vi.spyOn(backend, "planGraph").mockResolvedValue({ error: "", batch: 0, gaps: [], plan: { version: "1.564", actual: false, statements: [] } } as never);
    render(<PlansView run={run({ plans: [{ index: 0, mode: "estimated", batch: 0, execution: 1, statement: 1, bytes: 10 } as PlanInfo] })} />);
    expect(await screen.findByRole("combobox", { name: "Statement" })).toBeInTheDocument();
  });
});
