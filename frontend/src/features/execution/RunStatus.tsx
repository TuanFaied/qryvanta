import { CheckCircle2, CircleSlash, Clock, Loader2, XCircle } from "lucide-react";
import { useEffect, useState } from "react";
import { Outcome } from "@/services/backend";
import type { Run } from "@/stores/execution-store";

export function formatDuration(ms: number): string {
  if (ms < 1000) return `${Math.max(0, Math.round(ms))} ms`;
  if (ms < 60_000) return `${(ms / 1000).toFixed(ms < 10_000 ? 2 : 1)} s`;
  const m = Math.floor(ms / 60_000);
  const s = Math.floor((ms % 60_000) / 1000);
  return `${m} min ${s} s`;
}

export function rowCount(n: number): string {
  return `${n.toLocaleString("en-US")} ${n === 1 ? "row" : "rows"}`;
}

/** Elapsed time while running, then the server-measured duration. */
function useElapsed(run: Run): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (run.status !== "running") return;
    const id = window.setInterval(() => setNow(Date.now()), 100);
    return () => window.clearInterval(id);
  }, [run.status]);
  if (run.status === "running") return now - run.startedAt;
  return run.summary?.elapsedMs ?? (run.finishedAt ?? now) - run.startedAt;
}

/** One-line status of a run: what it is doing or how it ended, time, and row counts. */
export function RunStatus({ run }: { run: Run }) {
  const elapsed = useElapsed(run);
  const rows = run.resultSets.reduce((n, rs) => n + rs.rows.length, 0);
  const sets = run.resultSets.length;
  const outcome = run.summary?.outcome;
  const errors = run.summary?.errorCount ?? run.messages.filter((m) => m.kind === "error").length;

  let icon = <Loader2 className="size-3.5 animate-spin text-primary" />;
  let label = "Running";
  if (run.status === "finished") {
    if (run.error || outcome === Outcome.OutcomeFailed) {
      icon = <XCircle className="size-3.5 text-destructive" />;
      label = "Failed";
    } else if (outcome === Outcome.OutcomeCancelled) {
      icon = <CircleSlash className="size-3.5 text-muted-foreground" />;
      label = "Cancelled";
    } else if (outcome === Outcome.OutcomeTimedOut) {
      icon = <Clock className="size-3.5 text-warning" />;
      label = "Timed out";
    } else if (errors > 0) {
      icon = <XCircle className="size-3.5 text-destructive" />;
      label = `Completed with ${errors === 1 ? "1 error" : `${errors} errors`}`;
    } else {
      icon = <CheckCircle2 className="size-3.5 text-success" />;
      label = outcome === Outcome.OutcomeTruncated ? "Completed (limit reached)" : "Completed";
    }
  }

  return (
    <div
      role="status"
      aria-live="polite"
      className="flex min-w-0 items-center gap-2 overflow-hidden text-[11px] whitespace-nowrap text-muted-foreground"
    >
      {icon}
      <span className="text-foreground">{label}</span>
      <span>·</span>
      <span className="tabular-nums">{formatDuration(elapsed)}</span>
      <span>·</span>
      <span className="tabular-nums">{rowCount(rows)}</span>
      {sets > 1 && (
        <>
          <span>·</span>
          <span>{sets} result sets</span>
        </>
      )}
    </div>
  );
}
