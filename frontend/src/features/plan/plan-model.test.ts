import { describe, expect, it } from "vitest";
import type { PlanOperator, PlanStatement } from "@/services/backend";
import { costPercent, edgeRows, formatValue, layoutStatement, objectLabel, strokeWidth, uniqueClause, UNAVAILABLE } from "./plan-model";

export function operator(nodeId: number, extra: Partial<PlanOperator> = {}): PlanOperator {
  return {
    nodeId,
    physicalOp: "Clustered Index Scan",
    logicalOp: "Clustered Index Scan",
    parallel: false,
    estimated: { rows: 10, rowsRead: null, executions: 1, rowSize: 20, cpu: 0.001, io: 0.003, subtreeCost: 0.004 },
    actual: null,
    objects: [],
    predicates: [],
    output: [],
    warnings: [],
    children: [],
    ...extra,
  } as PlanOperator;
}

const tree = (): PlanStatement =>
  ({
    id: 1,
    text: "SELECT",
    type: "SELECT",
    root: operator(0, {
      physicalOp: "Nested Loops",
      estimated: { rows: 9, rowsRead: null, executions: 1, rowSize: 10, cpu: 0.001, io: 0, subtreeCost: 0.01 },
      children: [
        operator(1, { estimated: { rows: 12, rowsRead: null, executions: 1, rowSize: 10, cpu: 0, io: 0, subtreeCost: 0.003 } }),
        operator(2, { estimated: { rows: 1, rowsRead: null, executions: 12, rowSize: 10, cpu: 0, io: 0, subtreeCost: 0.005 } }),
      ],
    }),
  }) as unknown as PlanStatement;

describe("plan layout", () => {
  it("puts the root on the left and children to the right, centred on them", () => {
    const { nodes, edges } = layoutStatement(tree(), "rtl", false);
    const at = Object.fromEntries(nodes.map((n) => [n.id, n]));
    expect(at.n0!.x).toBe(0);
    expect(at.n1!.x).toBeGreaterThan(0);
    expect(at.n2!.y).toBeGreaterThan(at.n1!.y);
    expect(at.n0!.y).toBe((at.n1!.y + at.n2!.y) / 2);
    expect(edges).toEqual([
      { id: "e1-0", source: "n1", target: "n0", rows: 12 },
      { id: "e2-0", source: "n2", target: "n0", rows: 12 },
    ]);
  });

  it("mirrors for left to right", () => {
    const { nodes } = layoutStatement(tree(), "ltr", false);
    expect(nodes.find((n) => n.id === "n1")!.x).toBeLessThan(0);
  });

  it("handles a statement without a plan", () => {
    expect(layoutStatement({ root: null } as never, "rtl", false)).toEqual({ nodes: [], edges: [] });
  });

  it("lays out several hundred operators quickly", () => {
    let op = operator(0);
    for (let i = 1; i < 600; i++) op = operator(i, { children: [op, operator(10000 + i)] });
    const started = performance.now();
    const { nodes } = layoutStatement({ root: op } as unknown as PlanStatement, "rtl", false);
    expect(nodes).toHaveLength(1199);
    expect(performance.now() - started).toBeLessThan(200);
  });
});

describe("plan values", () => {
  it("uses measured rows in actual plans, and never invents them", () => {
    expect(edgeRows(operator(1, { actual: { rows: 7, executions: 1 } as never }), true)).toBe(7);
    expect(edgeRows(operator(1), true)).toBeNull();
    expect(edgeRows(operator(1, { estimated: { rows: 3, executions: 500 } as never }), false)).toBe(1500);
  });

  it("formats absent values as unavailable, not 0", () => {
    expect(formatValue(null)).toBe(UNAVAILABLE);
    expect(formatValue(undefined)).toBe(UNAVAILABLE);
    expect(formatValue(0)).toBe("0");
    expect(formatValue(12345)).toBe("12,345");
    expect(formatValue(0.00123, 4)).toBe("0.0012");
  });

  it("computes the operator's share of the statement cost", () => {
    const st = tree();
    expect(costPercent(st.root!, st.root!)).toBeCloseTo(20);
    expect(costPercent(st.root!.children![1]!, st.root!)).toBeCloseTo(50);
    expect(costPercent(operator(5, { estimated: { subtreeCost: null } as never }), st.root!)).toBeNull();
  });

  it("thickens arrows with the rows", () => {
    expect(strokeWidth(null)).toBe(1);
    expect(strokeWidth(0)).toBe(1);
    expect(strokeWidth(1000)).toBeGreaterThan(strokeWidth(10));
    expect(strokeWidth(1e12)).toBe(9);
  });

  it("labels objects and picks a clause only when it is unique", () => {
    expect(objectLabel(operator(1, { objects: [{ schema: "dbo", table: "Orders", index: "PK_Orders" }] as never }))).toBe("dbo.Orders (PK_Orders)");
    const c = { offset: 5, length: 3, line: 1, column: 6 };
    expect(uniqueClause(operator(1, { predicates: [{ kind: "seek", text: "", columns: [], clause: c }] as never }))).toEqual(c);
    expect(uniqueClause(operator(1, { predicates: [{ clause: c }, { clause: { ...c, offset: 9 } }] as never }))).toBeNull();
    expect(uniqueClause(operator(1))).toBeNull();
  });
});
