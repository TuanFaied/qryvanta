import { Copy } from "lucide-react";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import { backend, type PlanInfo } from "@/services/backend";
import type { Run } from "@/stores/execution-store";

function size(bytes: number): string {
  return bytes < 1024 ? `${bytes} B` : `${(bytes / 1024).toFixed(bytes < 10240 ? 1 : 0)} KB`;
}

function where(p: PlanInfo): string {
  const run = p.execution > 1 ? ` (run ${p.execution})` : "";
  return `Batch ${p.batch + 1}${run}, statement ${p.statement}`;
}

/**
 * The plans a run captured (architecture D6). Their XML stays in the backend and is copied on
 * request: SSMS opens it saved as a .sqlplan file. The plan view itself arrives in Phase 6.
 */
export function PlansView({ run }: { run: Run | null }) {
  const [copied, setCopied] = useState<number | null>(null);
  const [error, setError] = useState<string | null>(null);

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
  const summary = run.summary?.plan;
  const kind = run.plan === "estimated" ? "Estimated" : "Actual";

  const copy = async (p: PlanInfo) => {
    setError(null);
    try {
      await navigator.clipboard.writeText(await backend.planXml(run.runId, p.index));
      setCopied(p.index);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  return (
    <div className="h-full overflow-auto p-3 text-xs">
      {run.status === "running" && run.plans.length === 0 && (
        <p className="text-muted-foreground">{run.plan === "estimated" ? "Compiling…" : "Running…"}</p>
      )}
      {run.status === "finished" && run.plans.length === 0 && (
        <p role="status" className="text-muted-foreground">
          No plans: the server returned none (statements with errors, and batches of only SET or control-of-flow statements, have
          no plan).
        </p>
      )}
      {run.plans.length > 0 && (
        <ul aria-label={`${kind} plans`} className="divide-y rounded border">
          {run.plans.map((p) => (
            <li key={p.index} className="flex items-center gap-3 px-3 py-1.5">
              <span className="font-medium">{where(p)}</span>
              <span className="text-muted-foreground">
                {kind} · {size(p.bytes)}
              </span>
              <Button
                size="sm"
                variant="ghost"
                className="ml-auto h-6 gap-1 px-2 text-xs"
                onClick={() => void copy(p)}
                title="Copy the showplan XML (save it as .sqlplan to open it in SSMS)"
              >
                <Copy className="size-3.5" />
                {copied === p.index ? "Copied" : "Copy XML"}
              </Button>
            </li>
          ))}
        </ul>
      )}
      {summary && !summary.complete && summary.reason && (
        <p role="alert" className="mt-2 text-warning">
          Incomplete: {summary.reason}
        </p>
      )}
      {error && (
        <p role="alert" className="mt-2 text-destructive">
          {error}
        </p>
      )}
      <p className="mt-3 text-muted-foreground">
        The plan view arrives with the next issues of Phase 6. Until then, copy a plan's XML and open it as a .sqlplan file.
      </p>
    </div>
  );
}
