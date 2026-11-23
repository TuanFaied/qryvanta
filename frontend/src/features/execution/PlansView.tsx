import { PlanView } from "@/features/plan/PlanView";
import type { Run } from "@/stores/execution-store";

export interface PlansViewProps {
  run: Run | null;
  /** Selects a range of the run's tab in the editor. */
  onRevealRange?: (line: number, column: number, length: number) => void;
}

/**
 * The Execution plan tab (architecture D6): how to capture a plan, the run's progress, and then
 * the plan view.
 */
export function PlansView({ run, onRevealRange }: PlansViewProps) {
  if (!run || run.plan === "") {
    return (
      <div className="flex h-full items-center justify-center p-6 text-center text-xs leading-relaxed text-muted-foreground">
        <p className="max-w-md">
          No plan captured. <span className="font-medium text-foreground">Show estimated plan</span> (Ctrl+L) compiles the query
          without running it; <span className="font-medium text-foreground">Include actual plan</span> (Ctrl+M) captures the plans
          as Execute runs it.
        </p>
      </div>
    );
  }
  if (run.plans.length > 0) return <PlanView run={run} onRevealRange={onRevealRange} />;
  const summary = run.summary?.plan;
  return (
    <div className="h-full overflow-auto p-3 text-xs">
      {run.status === "running" ? (
        <p className="text-muted-foreground">{run.plan === "estimated" ? "Compiling…" : "Running…"}</p>
      ) : (
        <p role="status" className="text-muted-foreground">
          No plans: the server returned none (statements with errors, and batches of only SET or control-of-flow statements, have
          no plan).
        </p>
      )}
      {summary && !summary.complete && summary.reason && (
        <p role="alert" className="mt-2 text-warning">
          Incomplete: {summary.reason}
        </p>
      )}
    </div>
  );
}
