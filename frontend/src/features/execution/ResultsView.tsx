import { AlertTriangle, Table2 } from "lucide-react";
import { EmptyState } from "@/components/shell/EmptyState";
import { Outcome } from "@/services/backend";
import type { Run } from "@/stores/execution-store";
import { ResultGrid, ROW_HEIGHT } from "./ResultGrid";
import { rowCount } from "./RunStatus";

/** Grids of a run, one per result set. A single result set fills the panel. */
export function ResultsView({ run, maxRows }: { run: Run | null; maxRows: number }) {
  if (!run) {
    return (
      <EmptyState icon={Table2} title="No results">
        Rows returned by the query appear here. Run a query with Ctrl+Enter.
      </EmptyState>
    );
  }

  const truncated = run.summary?.outcome === Outcome.OutcomeTruncated;
  const sets = run.resultSets;

  return (
    <div className="flex h-full flex-col">
      {truncated && (
        <div role="alert" className="flex shrink-0 items-center gap-2 border-b border-warning/40 bg-warning/10 px-3 py-1.5 text-xs">
          <AlertTriangle className="size-3.5 shrink-0 text-warning" />
          {run.summary?.truncatedBy === "bytes"
            ? `Showing the first ${rowCount(run.summary.totalRows)}: the result size limit stopped reading and the query was cancelled.`
            : `Showing the first ${rowCount(maxRows)}: the row limit stopped reading and the query was cancelled.`}{" "}
          The limits are in Settings.
        </div>
      )}
      {sets.length === 0 ? (
        run.status === "running" ? (
          <div className="flex-1" />
        ) : (
          <EmptyState icon={Table2} title="No result sets">
            The batch did not return any rows. Row counts and messages are on the Messages tab.
          </EmptyState>
        )
      ) : sets.length === 1 ? (
        <div className="min-h-0 flex-1">
          <ResultGrid resultSet={sets[0]!} label="Result 1" />
        </div>
      ) : (
        <div className="min-h-0 flex-1 space-y-2 overflow-y-auto p-1">
          {sets.map((rs, i) => (
            <section key={rs.index} aria-label={`Result ${i + 1}`} className="rounded-sm border">
              <h3 className="border-b bg-panel px-2 py-0.5 text-[11px] text-muted-foreground">
                Result {i + 1} · {rowCount(rs.rows.length)}
              </h3>
              <ResultGrid
                resultSet={rs}
                label={`Result ${i + 1}`}
                height={Math.min(Math.max(rs.rows.length + 1, 3), 12) * ROW_HEIGHT + 16}
              />
            </section>
          ))}
        </div>
      )}
    </div>
  );
}
