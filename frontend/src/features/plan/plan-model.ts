import type { PlanOperator, PlanStatement } from "@/services/backend";

/**
 * Pure helpers for the plan view: tree layout, costs, arrow thickness and value labels. Values
 * that are not in the plan are shown as "unavailable", never as 0 (architecture D6).
 */

export const UNAVAILABLE = "unavailable";

/** Which way data flows. "rtl" is how SQL Server tools draw plans: the root on the left. */
export type PlanDirection = "rtl" | "ltr";

export const NODE_WIDTH = 200;
export const NODE_HEIGHT = 84;
const COLUMN_GAP = 70;
const ROW_GAP = 26;

export interface LaidOutNode {
  id: string;
  op: PlanOperator;
  x: number;
  y: number;
}

export interface LaidOutEdge {
  id: string;
  /** The child operator: data flows from it into its parent. */
  source: string;
  target: string;
  /** The rows the edge carries, or null when they are not in the plan. */
  rows: number | null;
}

export const nodeId = (op: PlanOperator) => `n${op.nodeId}`;

/** An operator's children (the generated types allow null entries; there are none). */
export function children(op: PlanOperator): PlanOperator[] {
  return (op.children ?? []).filter((c): c is PlanOperator => !!c);
}

/**
 * Lays a statement's operators out as a tree: depth on one axis, leaves stacked on the other, each
 * parent centred on its children. O(n), and only rerun when the statement or direction changes.
 */
export function layoutStatement(st: PlanStatement | null, direction: PlanDirection, actual: boolean) {
  const nodes: LaidOutNode[] = [];
  const edges: LaidOutEdge[] = [];
  if (!st?.root) return { nodes, edges };
  let nextLeaf = 0;
  const place = (op: PlanOperator, depth: number): number => {
    const kids = children(op);
    let y: number;
    if (kids.length === 0) {
      y = nextLeaf++ * (NODE_HEIGHT + ROW_GAP);
    } else {
      const ys = kids.map((c) => place(c, depth + 1));
      y = (ys[0]! + ys[ys.length - 1]!) / 2;
      for (const c of kids) {
        edges.push({ id: `e${c.nodeId}-${op.nodeId}`, source: nodeId(c), target: nodeId(op), rows: edgeRows(c, actual) });
      }
    }
    const x = depth * (NODE_WIDTH + COLUMN_GAP) * (direction === "rtl" ? 1 : -1);
    nodes.push({ id: nodeId(op), op, x, y });
    return y;
  };
  place(st.root, 0);
  return { nodes, edges };
}

/** The rows an operator passes to its parent: measured in actual plans, otherwise estimated for all executions. */
export function edgeRows(op: PlanOperator, actual: boolean): number | null {
  if (actual) return op.actual?.rows ?? null;
  const rows = op.estimated.rows;
  if (rows == null) return null;
  return rows * (op.estimated.executions ?? 1);
}

/** Arrow thickness grows with the logarithm of the rows (1 to 9 px); unknown rows are thin. */
export function strokeWidth(rows: number | null): number {
  if (rows == null) return 1;
  return Math.min(9, 1 + Math.log10(rows + 1) * 1.6);
}

/** The operator's own estimated cost: its subtree cost minus its children's. */
export function operatorCost(op: PlanOperator): number | null {
  const subtree = op.estimated.subtreeCost;
  if (subtree == null) return null;
  const below = children(op).reduce((sum, c) => sum + (c.estimated.subtreeCost ?? 0), 0);
  return Math.max(0, subtree - below);
}

/** The operator's share of the statement's estimated cost, in percent. */
export function costPercent(op: PlanOperator, root: PlanOperator | null): number | null {
  const own = operatorCost(op);
  const total = root?.estimated.subtreeCost;
  if (own == null || !total) return null;
  return (own / total) * 100;
}

/** Formats a value, or "unavailable" when the plan does not have it. */
export function formatValue(v: number | null | undefined, digits = 0): string {
  if (v == null || !Number.isFinite(v)) return UNAVAILABLE;
  if (digits === 0 && Number.isInteger(v)) return v.toLocaleString("en-US");
  return v.toLocaleString("en-US", { maximumFractionDigits: digits || 4 });
}

/** The object an operator reads, as schema.table.index or alias. */
export function objectLabel(op: PlanOperator): string {
  const o = (op.objects ?? [])[0];
  if (!o) return "";
  const table = [o.schema, o.table].filter(Boolean).join(".");
  const index = o.index ? ` (${o.index})` : "";
  return `${table}${index}`;
}

/** Counts every operator of a statement. */
export function countOperators(op: PlanOperator | null | undefined): number {
  if (!op) return 0;
  return 1 + children(op).reduce((n, c) => n + countOperators(c), 0);
}

/** The single clause the operator's predicates map to, or null when none or several. */
export function uniqueClause(op: PlanOperator) {
  const clauses = (op.predicates ?? []).map((p) => p.clause).filter((c): c is NonNullable<typeof c> => !!c);
  const distinct = clauses.filter((c, i) => clauses.findIndex((d) => d.offset === c.offset && d.length === c.length) === i);
  return distinct.length === 1 ? distinct[0]! : null;
}
