import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import type { NodeGap, PlanOperator } from "@/services/backend";
import { OperatorDetails } from "./OperatorDetails";
import { operator } from "./plan-model.test";

const valueRow = (label: string) => screen.getByRole("row", { name: new RegExp(`^${label}`) });

describe("OperatorDetails", () => {
  it("shows actual values of an estimated plan as unavailable, never 0", () => {
    render(<OperatorDetails op={operator(1)} actualPlan={false} gap={null} onShowClause={vi.fn()} />);
    const rows = within(valueRow("Rows \\(all executions\\)")).getAllByRole("cell");
    expect(rows[0]).toHaveTextContent("10");
    expect(rows[1]).toHaveTextContent("unavailable");
    expect(within(valueRow("Executions")).getAllByRole("cell")[1]).toHaveTextContent("unavailable");
    // Not estimated by SQL Server at all: a dash, not "unavailable".
    expect(within(valueRow("Elapsed time")).getAllByRole("cell")[0]).toHaveTextContent("—");
    expect(screen.getByText(/An estimated plan has no actual values/)).toBeInTheDocument();
    expect(screen.queryByText(/^0$/)).not.toBeInTheDocument();
  });

  it("shows measured values, including real zeros, side by side with the estimates", () => {
    const op = operator(2, {
      estimated: { rows: 3, rowsRead: null, executions: 500, rowSize: 10, cpu: 0.1, io: 0.2, subtreeCost: 0.5 },
      actual: { rows: 0, rowsRead: null, executions: 500, elapsedMs: null, cpuMs: null, logicalReads: 1000, threads: 1 },
    } as Partial<PlanOperator>);
    render(<OperatorDetails op={op} actualPlan gap={null} onShowClause={vi.fn()} />);
    const rows = within(valueRow("Rows \\(all executions\\)")).getAllByRole("cell");
    expect(rows[0]).toHaveTextContent("1,500");
    expect(rows[1]).toHaveTextContent(/^0$/);
    expect(within(valueRow("Logical reads")).getAllByRole("cell")[1]).toHaveTextContent("1,000");
    expect(within(valueRow("Elapsed time")).getAllByRole("cell")[1]).toHaveTextContent("unavailable");
  });

  it("says when an actual plan has no counters for the operator", () => {
    render(<OperatorDetails op={operator(3, { physicalOp: "Compute Scalar" })} actualPlan gap={null} onShowClause={vi.fn()} />);
    expect(screen.getByText("SQL Server reported no run-time counters for this operator.")).toBeInTheDocument();
  });

  it("renders warnings, predicates with their clause, output columns and the gap", async () => {
    const onShowClause = vi.fn();
    const clause = { offset: 40, length: 12, line: 2, column: 7 };
    const op = operator(4, {
      warnings: [{ kind: "spillToTempDb", text: "Operator used tempdb to spill data during execution." }],
      predicates: [
        { kind: "seek", text: "p.ProductID = [oi].[OrderItemID]", columns: [], clause },
        { kind: "residual", text: "[oi].[OrderID]=(103)", columns: [] },
      ],
      output: [{ table: "Products", alias: "p", name: "Name" }, { name: "Expr1002" }],
    } as Partial<PlanOperator>);
    const gap = { statement: 0, nodeId: 4, estimatedRows: 10, actualRows: 5000, factor: 500, big: true, under: true } as NodeGap;
    render(<OperatorDetails op={op} actualPlan gap={gap} onShowClause={onShowClause} />);
    expect(screen.getByRole("region", { name: "Warnings" })).toHaveTextContent("spill data");
    const preds = screen.getByRole("region", { name: "Predicates" });
    expect(preds).toHaveTextContent("Seek: p.ProductID = [oi].[OrderItemID]");
    expect(preds).toHaveTextContent("Predicate: [oi].[OrderID]=(103)");
    await userEvent.click(within(preds).getByRole("button", { name: "Show in editor" }));
    expect(onShowClause).toHaveBeenCalledWith(clause);
    expect(screen.getByRole("region", { name: "Output columns" })).toHaveTextContent("p.Name");
    expect(screen.getByRole("region", { name: "Output columns" })).toHaveTextContent("Expr1002");
    expect(screen.getByRole("alert")).toHaveTextContent("Estimated 10 rows, actual 5,000: under-estimated ×500 (a large gap).");
  });
});
