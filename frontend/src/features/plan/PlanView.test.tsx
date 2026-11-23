import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { backend, type PlanGraph, type PlanInfo } from "@/services/backend";
import type { Run } from "@/stores/execution-store";
import { PlanView } from "./PlanView";
import { operator } from "./plan-model.test";

const clause = { offset: 30, length: 9, line: 2, column: 7 };

const graph = (batch: number, statements: { text: string; type: string; root: unknown }[], actual = false): PlanGraph =>
  ({ error: "", batch, gaps: [], plan: { version: "1.564", build: "", actual, statements } }) as unknown as PlanGraph;

const seekWithClause = operator(0, {
  physicalOp: "Clustered Index Seek",
  objects: [{ schema: "dbo", table: "Orders", index: "PK_Orders" }],
  warnings: [{ kind: "implicitConversion", text: "Type conversion" }],
  predicates: [{ kind: "seek", text: "o.OrderID = (103)", columns: [], clause }],
} as never);

const run = (plans: PlanInfo[], extra: Partial<Run> = {}): Run =>
  ({
    runId: "r1",
    tabId: "t1",
    connectionId: "c1",
    database: "Sales",
    status: "finished",
    startedAt: 0,
    finishedAt: 1,
    lineOffset: 9,
    columnOffset: 4,
    batches: [],
    resultSets: [],
    messages: [],
    plan: "estimated",
    plans,
    summary: null,
    error: null,
    ...extra,
  }) as Run;

const info = (index: number, batch: number) => ({ index, mode: "estimated", batch, execution: 1, statement: 1, bytes: 100 }) as PlanInfo;

afterEach(() => vi.restoreAllMocks());

describe("PlanView", () => {
  it("lists statements by batch and switches between them", async () => {
    vi.spyOn(backend, "planGraph").mockImplementation(async (_run, index) =>
      index === 0
        ? graph(0, [
            { text: "SET NOCOUNT ON", type: "SET ON/OFF", root: null },
            { text: "SELECT * FROM dbo.Orders", type: "SELECT", root: seekWithClause },
          ])
        : graph(1, [{ text: "SELECT 2", type: "SELECT", root: operator(0, { physicalOp: "Constant Scan" } as never) }]),
    );
    render(<PlanView run={run([info(0, 0), info(1, 1)])} />);
    const picker = await screen.findByRole("combobox", { name: "Statement" });
    await waitFor(() => expect(picker.querySelectorAll("option")).toHaveLength(3));
    expect([...picker.querySelectorAll("optgroup")].map((g) => g.getAttribute("label"))).toEqual(["Batch 1", "Batch 2"]);

    // The first statement has no query plan.
    expect(screen.getByText("This statement has no query plan (SET ON/OFF).")).toBeInTheDocument();
    await userEvent.selectOptions(picker, "0:1");
    expect(await screen.findByLabelText("Clustered Index Seek, node 0")).toBeInTheDocument();
    expect(screen.getByText("Arrow thickness: estimated rows (all executions)")).toBeInTheDocument();
    await userEvent.selectOptions(picker, "1:0");
    expect(await screen.findByLabelText("Constant Scan, node 0")).toBeInTheDocument();
  });

  it("selects an operator, shows its details and its clause in the editor", async () => {
    vi.spyOn(backend, "planGraph").mockResolvedValue(graph(0, [{ text: "SELECT", type: "SELECT", root: seekWithClause }], true));
    const onRevealRange = vi.fn();
    render(<PlanView run={run([info(0, 0)], { plan: "actual" })} onRevealRange={onRevealRange} />);
    const node = await screen.findByLabelText("Clustered Index Seek, node 0");
    expect(screen.getByLabelText("1 warnings")).toBeInTheDocument();
    expect(screen.getByText("Arrow thickness: actual rows")).toBeInTheDocument();
    // A plain click: user-event's mousedown has no view, which d3-zoom (inside React Flow) reads.
    fireEvent.click(node);
    expect(screen.getByRole("complementary", { name: "Operator details" })).toHaveTextContent("dbo.Orders (PK_Orders)");
    // Clause line 2 of the run's SQL is editor line 2 + 9; columns shift only on the first line.
    expect(onRevealRange).toHaveBeenCalledWith(11, 7, 9);
  });

  it("copies and saves the plan's XML", async () => {
    vi.spyOn(backend, "planGraph").mockResolvedValue(graph(0, [{ text: "SELECT", type: "SELECT", root: seekWithClause }]));
    vi.spyOn(backend, "planXml").mockResolvedValue("<ShowPlanXML/>");
    const save = vi.spyOn(backend, "savePlanAs").mockResolvedValue({ path: "C:\\p.sqlplan", name: "p.sqlplan", error: "", cancelled: false } as never);
    const write = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", { value: { writeText: write }, configurable: true });
    render(<PlanView run={run([info(0, 0)])} />);
    await userEvent.click(await screen.findByRole("button", { name: /Copy XML/ }));
    expect(write).toHaveBeenCalledWith("<ShowPlanXML/>");
    await userEvent.click(screen.getByRole("button", { name: /Save .sqlplan/ }));
    expect(save).toHaveBeenCalledWith("Plan 1", "<ShowPlanXML/>");
    expect(await screen.findByText("Saved p.sqlplan: open it in SSMS.")).toBeInTheDocument();
  });

  it("shows why a plan cannot be shown, and incomplete plans", async () => {
    vi.spyOn(backend, "planGraph").mockResolvedValue({ error: "The plan cannot be shown: unsupported showplan version", plan: null, batch: 0, gaps: [] } as never);
    const summary = { plan: { mode: "actual", count: 1, tooLarge: 0, complete: false, reason: "The run ended early (cancelled)." } } as never;
    render(<PlanView run={run([info(0, 0)], { summary })} />);
    expect(await screen.findByText(/unsupported showplan version/)).toBeInTheDocument();
    expect(screen.getByText("Incomplete: The run ended early (cancelled).")).toBeInTheDocument();
  });
});
