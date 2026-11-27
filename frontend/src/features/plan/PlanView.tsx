import { ArrowLeftRight, Copy, Save } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { Button } from "@/components/ui/button";
import { backend, type NodeGap, type PlanClause, type PlanGraph, type PlanOperator, type PlanStatement } from "@/services/backend";
import type { Run } from "@/stores/execution-store";
import { OperatorDetails } from "./OperatorDetails";
import { PlanDiagram } from "./PlanDiagram";
import { countOperators, nodeId, uniqueClause, type PlanDirection } from "./plan-model";

export interface PlanViewProps {
  run: Run;
  /** Selects a range of the run's tab in the editor (1-based editor line and column). */
  onRevealRange?: (line: number, column: number, length: number) => void;
}

interface Choice {
  doc: number;
  statement: number;
}

/** Statements longer than this are cut in the picker. */
const LABEL_LENGTH = 70;

/**
 * The plans of a run as graphs (architecture D6). Every number is labelled estimated or actual,
 * and a missing one reads "unavailable".
 */
export function PlanView({ run, onRevealRange }: PlanViewProps) {
  const [graphs, setGraphs] = useState<Record<number, PlanGraph>>({});
  const [choice, setChoice] = useState<Choice>({ doc: 0, statement: 0 });
  const [direction, setDirection] = useState<PlanDirection>("rtl");
  const [selected, setSelected] = useState<PlanOperator | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  // Load each plan's graph once; new plans arrive while an actual run is still going.
  useEffect(() => {
    let cancelled = false;
    for (const p of run.plans) {
      if (graphs[p.index]) continue;
      backend
        .planGraph(run.runId, p.index)
        .then((g) => !cancelled && setGraphs((all) => ({ ...all, [p.index]: g })))
        .catch((err: unknown) =>
          !cancelled &&
          setGraphs((all) => ({ ...all, [p.index]: { error: err instanceof Error ? err.message : String(err), plan: null, batch: p.batch, gaps: [] } })),
        );
    }
    return () => {
      cancelled = true;
    };
  }, [run.runId, run.plans, graphs]);

  // Every statement of every plan, grouped by batch, for the picker.
  const options = useMemo(() => {
    const out: { batch: number; choice: Choice; label: string }[] = [];
    for (const p of run.plans) {
      const g = graphs[p.index];
      const statements = g?.plan?.statements ?? [];
      statements.forEach((st, i) => {
        if (!st) return;
        const text = st.text.replace(/\s+/g, " ").trim();
        const short = text.length > LABEL_LENGTH ? `${text.slice(0, LABEL_LENGTH)}…` : text;
        out.push({ batch: p.batch, choice: { doc: p.index, statement: i }, label: `${st.type || "Statement"} · ${short}` });
      });
    }
    return out;
  }, [run.plans, graphs]);

  const graph = graphs[choice.doc];
  const statement: PlanStatement | null = graph?.plan?.statements?.[choice.statement] ?? null;
  const actual = !!graph?.plan?.actual;
  const gaps = useMemo(() => {
    const m = new Map<number, NodeGap>();
    for (const g of graph?.gaps ?? []) if (g.statement === choice.statement) m.set(g.nodeId, g);
    return m;
  }, [graph, choice.statement]);

  // The run's SQL may be a selection: map clause positions to the editor, as messages do.
  const reveal = (c: PlanClause) => {
    const line = c.line + run.lineOffset;
    const column = c.line === 1 ? c.column + (run.columnOffset ?? 0) : c.column;
    onRevealRange?.(line, column, c.length);
  };

  const select = (op: PlanOperator) => {
    setSelected(op);
    const clause = uniqueClause(op);
    if (clause) reveal(clause);
  };

  const copyXml = async () => {
    setNotice(null);
    try {
      await navigator.clipboard.writeText(await backend.planXml(run.runId, choice.doc));
      setNotice("Copied the plan's XML.");
    } catch (err) {
      setNotice(err instanceof Error ? err.message : String(err));
    }
  };
  const save = async () => {
    setNotice(null);
    try {
      const res = await backend.savePlanAs(`Plan ${choice.doc + 1}`, await backend.planXml(run.runId, choice.doc));
      if (res.error) setNotice(res.error);
      else if (!res.cancelled) setNotice(`Saved ${res.name}: open it in SSMS.`);
    } catch (err) {
      setNotice(err instanceof Error ? err.message : String(err));
    }
  };

  const batches = [...new Set(options.map((o) => o.batch))];
  const value = `${choice.doc}:${choice.statement}`;
  const summary = run.summary?.plan;

  return (
    <div className="flex h-full flex-col text-xs">
      <div className="flex shrink-0 flex-wrap items-center gap-2 border-b px-2 py-1">
        <label className="flex min-w-0 flex-1 items-center gap-1.5">
          <span className="shrink-0 text-muted-foreground">Statement</span>
          <select
            aria-label="Statement"
            className="h-6 min-w-0 flex-1 rounded border bg-background px-1"
            value={value}
            onChange={(e) => {
              const [doc, st] = e.target.value.split(":").map(Number);
              setChoice({ doc: doc ?? 0, statement: st ?? 0 });
              setSelected(null);
            }}
          >
            {batches.map((b) => (
              <optgroup key={b} label={`Batch ${b + 1}`}>
                {options
                  .filter((o) => o.batch === b)
                  .map((o) => (
                    <option key={`${o.choice.doc}:${o.choice.statement}`} value={`${o.choice.doc}:${o.choice.statement}`}>
                      {o.label}
                    </option>
                  ))}
              </optgroup>
            ))}
          </select>
        </label>
        <span className="text-muted-foreground">{actual ? "Actual plan" : "Estimated plan"}</span>
        <Button
          size="sm"
          variant="ghost"
          className="h-6 gap-1 px-2 text-xs"
          onClick={() => setDirection((d) => (d === "rtl" ? "ltr" : "rtl"))}
          title="Switch the direction the data flows"
        >
          <ArrowLeftRight className="size-3.5" />
          {direction === "rtl" ? "Right to left" : "Left to right"}
        </Button>
        <Button size="sm" variant="ghost" className="h-6 gap-1 px-2 text-xs" onClick={() => void copyXml()}>
          <Copy className="size-3.5" />
          Copy XML
        </Button>
        <Button size="sm" variant="ghost" className="h-6 gap-1 px-2 text-xs" onClick={() => void save()}>
          <Save className="size-3.5" />
          Save .sqlplan
        </Button>
      </div>
      {summary && !summary.complete && summary.reason && (
        <p role="alert" className="shrink-0 px-2 py-1 text-warning">
          Incomplete: {summary.reason}
        </p>
      )}
      {notice && (
        <p role="status" className="shrink-0 px-2 py-1 text-muted-foreground">
          {notice}
        </p>
      )}
      <div className="flex min-h-0 flex-1">
        <div className="min-w-0 flex-1">
          {!graph && <p className="p-3 text-muted-foreground">Loading the plan…</p>}
          {graph?.error && (
            <p role="alert" className="p-3 text-destructive">
              {graph.error}
            </p>
          )}
          {statement && !statement.root && (
            <p className="p-3 text-muted-foreground">This statement has no query plan ({statement.type}).</p>
          )}
          {statement?.root && (
            <PlanDiagram
              statement={statement}
              actual={actual}
              direction={direction}
              gaps={gaps}
              selected={selected ? nodeId(selected) : null}
              onSelect={select}
            />
          )}
        </div>
        <aside aria-label="Operator details" className="w-72 shrink-0 overflow-auto border-l p-3">
          {selected ? (
            <OperatorDetails op={selected} actualPlan={actual} gap={gaps.get(selected.nodeId) ?? null} onShowClause={reveal} />
          ) : (
            <p className="text-muted-foreground">
              {statement?.root
                ? `Select an operator (${countOperators(statement.root)} in this statement) to see its estimated and actual values.`
                : "Select a statement with a query plan."}
            </p>
          )}
        </aside>
      </div>
    </div>
  );
}
