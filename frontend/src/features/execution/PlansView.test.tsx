import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { backend, type PlanInfo } from "@/services/backend";
import type { Run } from "@/stores/execution-store";
import { PlansView } from "./PlansView";

const plan = (index: number, extra: Partial<PlanInfo> = {}): PlanInfo =>
  ({ index, mode: "estimated", batch: 0, execution: 1, statement: index + 1, bytes: 2048, ...extra }) as PlanInfo;

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

describe("PlansView", () => {
  it("explains how to capture a plan", () => {
    render(<PlansView run={null} />);
    expect(screen.getByText(/No plan captured/)).toHaveTextContent("Show estimated plan (Ctrl+L)");
    expect(screen.getByText(/No plan captured/)).toHaveTextContent("Include actual plan (Ctrl+M)");
  });

  it("lists the plans with their batch, statement, kind and size", () => {
    render(<PlansView run={run({ plans: [plan(0), plan(1, { batch: 1, execution: 2, bytes: 300 })] })} />);
    const list = screen.getByRole("list", { name: "Estimated plans" });
    expect(list).toHaveTextContent("Batch 1, statement 1");
    expect(list).toHaveTextContent("Estimated · 2.0 KB");
    expect(list).toHaveTextContent("Batch 2 (run 2), statement 2");
    expect(list).toHaveTextContent("300 B");
  });

  it("copies a plan's XML, fetched from the backend", async () => {
    const xml = vi.spyOn(backend, "planXml").mockResolvedValue("<ShowPlanXML/>");
    const write = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", { value: { writeText: write }, configurable: true });
    render(<PlansView run={run({ plan: "actual", plans: [plan(0, { mode: "actual" as PlanInfo["mode"] })] })} />);
    await userEvent.click(screen.getByRole("button", { name: /Copy XML/ }));
    expect(xml).toHaveBeenCalledWith("r1", 0);
    expect(write).toHaveBeenCalledWith("<ShowPlanXML/>");
    expect(screen.getByRole("button", { name: /Copied/ })).toBeInTheDocument();
  });

  it("says when the plans are incomplete, and when there are none", () => {
    const summary = { plan: { mode: "actual", count: 1, tooLarge: 0, complete: false, reason: "The run ended early (timed_out)." } } as never;
    const { unmount } = render(<PlansView run={run({ plan: "actual", plans: [plan(0)], summary })} />);
    expect(screen.getByRole("alert")).toHaveTextContent("Incomplete: The run ended early (timed_out).");
    unmount();
    render(<PlansView run={run()} />);
    expect(screen.getByRole("status")).toHaveTextContent("No plans: the server returned none");
  });
});
