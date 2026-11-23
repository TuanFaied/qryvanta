import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import type { Analysis, Probe } from "@/services/backend";
import type { ProbeState } from "@/stores/analysis-store";
import { ProbesSection } from "./ProbesSection";

const probe = (id: string, extra: Partial<Probe> = {}): Probe =>
  ({
    id,
    kind: "fanOut" as Probe["kind"],
    title: `Rows per OrderID in dbo.Payments (${id})`,
    proves: "Whether a value of OrderID appears in more than one dbo.Payments row.",
    sql: `SELECT COUNT_BIG(*) AS key_values /* ${id} */ FROM [Sales].[dbo].[Payments]`,
    params: [],
    findings: [0],
    overBudget: false,
    ...extra,
  }) as Probe;

const analysis = (probes: Probe[]): Analysis =>
  ({
    id: "a1",
    available: true,
    reason: "",
    findings: [],
    failures: [],
    rules: 1,
    statements: 1,
    syntaxErrors: 0,
    metadata: { checked: true, reason: "", unresolved: [] },
    probes,
    steps: [],
    stopped: false,
  }) as Analysis;

const idle: ProbeState = { running: false, results: [], dismissed: [], error: null };

function setup(probes: Probe[], state: ProbeState = idle, current = true) {
  const onRun = vi.fn();
  const onCancel = vi.fn();
  render(<ProbesSection result={analysis(probes)} probes={state} current={current} onRun={onRun} onCancel={onCancel} />);
  return { onRun, onCancel };
}

describe("ProbesSection", () => {
  it("explains probes when there are none", () => {
    setup([]);
    expect(screen.getByText(/Probes are read-only queries that measure what a finding claims/)).toBeInTheDocument();
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });

  it("lists every probe in full, with its parameters, before anything runs", () => {
    setup([probe("p1"), probe("p2", { params: [{ name: "p1", value: "2026-01-31" }, { name: "p2", value: "2026-02-01" }] })]);
    expect(screen.getByLabelText("SQL of p1")).toHaveTextContent("SELECT COUNT_BIG(*) AS key_values /* p1 */ FROM [Sales].[dbo].[Payments]");
    expect(screen.getByLabelText("SQL of p2")).toBeInTheDocument();
    expect(screen.getByText("@p1 = '2026-01-31', @p2 = '2026-02-01'")).toBeInTheDocument();
    expect(screen.getAllByText(/Whether a value of OrderID appears/)).toHaveLength(2);
  });

  it("runs the approved probes only when the user clicks", async () => {
    const { onRun } = setup([probe("p1"), probe("p2")]);
    expect(onRun).not.toHaveBeenCalled();
    await userEvent.click(screen.getByRole("button", { name: "Run 2 probes" }));
    expect(onRun).toHaveBeenCalledWith(["p1", "p2"]);
  });

  it("leaves probes over budget out of the approval", async () => {
    const { onRun } = setup([probe("p1"), probe("p2", { overBudget: true }), probe("p3", { overBudget: true })]);
    expect(screen.getAllByText("· over budget, not run")).toHaveLength(2);
    expect(screen.getByText(/2 probes are over the probe budget and will not run/)).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Run 1 probe" }));
    expect(onRun).toHaveBeenCalledWith(["p1"]);
  });

  it("cannot run when every probe is over budget, or from another tab", () => {
    setup([probe("p1", { overBudget: true })]);
    expect(screen.getByRole("button", { name: "Run 0 probes" })).toBeDisabled();
  });

  it("cannot run the probes of another tab's analysis", () => {
    setup([probe("p1")], idle, false);
    expect(screen.getByRole("button", { name: "Run 1 probe" })).toBeDisabled();
  });

  it("cancels running probes", async () => {
    const { onCancel } = setup([probe("p1"), probe("p2")], { ...idle, running: true });
    expect(screen.getByRole("status")).toHaveTextContent("Running 2 probes…");
    expect(screen.queryByRole("button", { name: /Run/ })).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(onCancel).toHaveBeenCalledOnce();
  });

  it("shows results, failures and dismissed findings", () => {
    setup([probe("p1"), probe("p2")], {
      running: false,
      results: [
        { probeId: "p1", elapsedMs: 12, error: "", summary: "3 of 12 rows have no match (measured).", showsProblem: true, values: {} },
        { probeId: "p2", elapsedMs: 15000, error: "the query timed out", summary: "", showsProblem: false, values: {} },
      ],
      dismissed: [{ finding: { title: "Neither side is unique" } as never, reason: "Each value appears in one row." }],
      error: null,
    });
    expect(screen.getByText("3 of 12 rows have no match (measured).")).toBeInTheDocument();
    expect(screen.getByText("Failed: the query timed out")).toBeInTheDocument();
    expect(screen.getByText("Dismissed by the probes")).toBeInTheDocument();
    expect(screen.getByText(/Each value appears in one row/)).toBeInTheDocument();
    // A run is approved once: analyze again to run again.
    expect(screen.getByRole("button", { name: "Run 2 probes" })).toBeDisabled();
  });

  it("shows why the probes could not run", () => {
    setup([probe("p1")], { ...idle, error: "Cancelled: no findings were changed." });
    expect(screen.getByRole("alert")).toHaveTextContent("Cancelled: no findings were changed.");
  });
});
