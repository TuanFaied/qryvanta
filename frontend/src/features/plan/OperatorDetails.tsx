import { useVirtualizer } from "@tanstack/react-virtual";
import { AlertTriangle } from "lucide-react";
import { useRef } from "react";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import type { NodeGap, PlanClause, PlanColumn, PlanOperator } from "@/services/backend";
import { formatValue, objectLabel } from "./plan-model";

/** Not estimated (or not measured) by SQL Server at all: different from a missing value. */
const NOT_APPLICABLE = "—";

const predicateLabel: Record<string, string> = {
  seek: "Seek",
  residual: "Predicate",
  join: "Join",
  hashBuild: "Hash keys (build)",
  hashProbe: "Hash keys (probe)",
};

export interface OperatorDetailsProps {
  op: PlanOperator;
  /** True for an actual plan; then missing measured values read "unavailable". */
  actualPlan: boolean;
  gap: NodeGap | null;
  onShowClause: (clause: PlanClause) => void;
}

/**
 * The selected operator: estimated and actual values side by side, each labelled. A value the
 * plan does not have is "unavailable", never 0 (architecture D6).
 */
export function OperatorDetails({ op, actualPlan, gap, onShowClause }: OperatorDetailsProps) {
  const est = op.estimated;
  const act = op.actual;
  const estAll = est.rows == null ? null : est.rows * (est.executions ?? 1);
  const rows: [string, string, string][] = [
    ["Rows (all executions)", formatValue(estAll, 2), formatValue(act?.rows)],
    ["Rows per execution", formatValue(est.rows, 2), NOT_APPLICABLE],
    ["Executions", formatValue(est.executions, 2), formatValue(act?.executions)],
    ["Rows read", formatValue(est.rowsRead, 2), formatValue(act?.rowsRead)],
    ["Elapsed time (ms)", NOT_APPLICABLE, formatValue(act?.elapsedMs)],
    ["CPU time (ms)", NOT_APPLICABLE, formatValue(act?.cpuMs)],
    ["Logical reads", NOT_APPLICABLE, formatValue(act?.logicalReads)],
    ["Row size (bytes)", formatValue(est.rowSize), NOT_APPLICABLE],
    ["CPU cost", formatValue(est.cpu, 6), NOT_APPLICABLE],
    ["I/O cost", formatValue(est.io, 6), NOT_APPLICABLE],
    ["Subtree cost", formatValue(est.subtreeCost, 6), NOT_APPLICABLE],
  ];
  const warnings = op.warnings ?? [];
  const predicates = op.predicates ?? [];

  return (
    <div className="space-y-3 text-xs" aria-label={`Operator ${op.nodeId}`}>
      <div>
        <p className="text-sm font-semibold">{op.physicalOp}</p>
        <p className="text-muted-foreground">
          {op.logicalOp !== op.physicalOp && <>{op.logicalOp} · </>}Node {op.nodeId}
          {op.parallel && " · parallel"}
          {op.executionMode && ` · ${op.executionMode} mode`}
        </p>
        {objectLabel(op) && <p className="mt-0.5 font-mono">{objectLabel(op)}</p>}
      </div>

      <table className="w-full border-collapse" aria-label="Estimated and actual values">
        <thead>
          <tr className="text-[11px] text-muted-foreground">
            <th className="py-0.5 text-left font-medium" />
            <th className="py-0.5 text-right font-medium">Estimated</th>
            <th className="py-0.5 text-right font-medium">Actual</th>
          </tr>
        </thead>
        <tbody>
          {rows.map(([label, e, a]) => (
            <tr key={label} className="border-t">
              <th scope="row" className="py-0.5 text-left font-normal text-muted-foreground">
                {label}
              </th>
              <td className={cn("py-0.5 text-right font-mono", e === "unavailable" && "text-muted-foreground italic")}>{e}</td>
              <td className={cn("py-0.5 text-right font-mono", a === "unavailable" && "text-muted-foreground italic")}>{a}</td>
            </tr>
          ))}
        </tbody>
      </table>
      {!actualPlan && <p className="text-muted-foreground">An estimated plan has no actual values: include the actual plan (Ctrl+M) and run the query.</p>}
      {actualPlan && !act && <p className="text-muted-foreground">SQL Server reported no run-time counters for this operator.</p>}

      {gap && (
        <p role={gap.big ? "alert" : undefined} className={cn(gap.big ? "text-warning" : "text-muted-foreground")}>
          Estimated {formatValue(gap.estimatedRows, 2)} rows, actual {formatValue(gap.actualRows)}: {gap.under ? "under" : "over"}-estimated
          ×{formatValue(gap.factor, 1)}
          {gap.big && " (a large gap)"}.
        </p>
      )}

      {warnings.length > 0 && (
        <section aria-label="Warnings">
          <p className="mb-0.5 font-medium">Warnings</p>
          <ul className="space-y-1">
            {warnings.map((w, i) => (
              <li key={i} className="flex gap-1.5 text-warning">
                <AlertTriangle className="mt-0.5 size-3.5 shrink-0" />
                <span>{w.text}</span>
              </li>
            ))}
          </ul>
        </section>
      )}

      {predicates.length > 0 && (
        <section aria-label="Predicates">
          <p className="mb-0.5 font-medium">Predicates</p>
          <ul className="space-y-1.5">
            {predicates.map((p, i) => (
              <li key={i}>
                <span className="text-muted-foreground">{predicateLabel[p.kind] ?? p.kind}: </span>
                <code className="font-mono break-all">{p.text || "(no text)"}</code>
                {p.clause && (
                  <Button size="sm" variant="link" className="h-auto p-0 pl-1 text-xs" onClick={() => onShowClause(p.clause!)}>
                    Show in editor
                  </Button>
                )}
              </li>
            ))}
          </ul>
        </section>
      )}

      <OutputColumns columns={(op.output ?? []).filter((c): c is PlanColumn => !!c)} />
    </div>
  );
}

function columnLabel(c: PlanColumn): string {
  const owner = c.alias || c.table;
  return owner ? `${owner}.${c.name}` : c.name;
}

/** Output columns, virtualized: operators of wide queries output hundreds. */
function OutputColumns({ columns }: { columns: PlanColumn[] }) {
  const parentRef = useRef<HTMLDivElement>(null);
  const virtual = useVirtualizer({ count: columns.length, getScrollElement: () => parentRef.current, estimateSize: () => 18, overscan: 8 });
  if (columns.length === 0) return null;
  const items = virtual.getVirtualItems();
  // jsdom (tests) measures nothing: show the first rows directly.
  const shown = items.length > 0 ? items : columns.slice(0, 50).map((_, index) => ({ index, start: index * 18, key: index }));
  return (
    <section aria-label="Output columns">
      <p className="mb-0.5 font-medium">Output columns ({columns.length})</p>
      <div ref={parentRef} className="max-h-48 overflow-auto rounded border">
        <div style={{ height: virtual.getTotalSize() || shown.length * 18, position: "relative" }}>
          {shown.map((item) => (
            <div
              key={item.key}
              className="absolute left-0 w-full truncate px-2 font-mono leading-[18px]"
              style={{ top: item.start, height: 18 }}
            >
              {columnLabel(columns[item.index]!)}
            </div>
          ))}
        </div>
      </div>
    </section>
  );
}
