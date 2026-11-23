import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import type { Analysis, Probe, ProbeResult } from "@/services/backend";
import type { ProbeState } from "@/stores/analysis-store";

export interface ProbesSectionProps {
  result: Analysis | null;
  probes: ProbeState;
  /** False when the analysis is for another tab: its probes are not run from here. */
  current: boolean;
  onRun: (probeIds: string[]) => void;
  onCancel: () => void;
}

const note = "px-7 pt-0.5 pb-3 text-xs leading-relaxed text-muted-foreground";

/**
 * The probes of an analysis (architecture D4): every query is shown in full, with what it would
 * prove, and nothing runs until the user approves with "Run N probes".
 */
export function ProbesSection({ result, probes: state, current, onRun, onCancel }: ProbesSectionProps) {
  const list = result?.available ? (result.probes ?? []) : [];
  if (list.length === 0) {
    return (
      <p className={note}>
        Probes are read-only queries that measure what a finding claims (fan-out, dropped rows, NULLs). They are proposed when a
        connected analysis has findings they can check, and run only when you approve them.
      </p>
    );
  }
  const runnable = list.filter((p) => !p.overBudget);
  const over = list.length - runnable.length;
  const results = new Map(state.results.map((r) => [r.probeId, r]));
  const ran = state.results.length > 0;

  return (
    <div className="space-y-2 px-3 pt-0.5 pb-3 text-xs">
      <p className="px-4 leading-relaxed text-muted-foreground">
        These read-only queries would collect evidence for the findings. They run under the read-only guard with the probe timeout,
        and only when you approve them.
      </p>
      <ol className="space-y-2" aria-label="Probe queries">
        {list.map((p) => (
          <ProbeItem key={p.id} probe={p} result={results.get(p.id)} />
        ))}
      </ol>
      {over > 0 && (
        <p className="px-4 text-warning">
          {over === 1 ? "1 probe is" : `${over} probes are`} over the probe budget and will not run. Raise "Maximum probe queries per run"
          in Settings to include {over === 1 ? "it" : "them"}.
        </p>
      )}
      <div className="flex items-center gap-2 px-4">
        {state.running ? (
          <>
            <span role="status" className="text-muted-foreground">
              Running {runnable.length === 1 ? "1 probe" : `${runnable.length} probes`}…
            </span>
            <Button size="sm" variant="outline" className="h-6 px-2 text-xs" onClick={onCancel}>
              Cancel
            </Button>
          </>
        ) : (
          <Button
            size="sm"
            className="h-6 px-2 text-xs"
            disabled={runnable.length === 0 || !current || ran}
            onClick={() => onRun(runnable.map((p) => p.id))}
            title={!current ? "Switch to the analyzed tab to run its probes" : ran ? "Analyze again to run probes again" : undefined}
          >
            Run {runnable.length === 1 ? "1 probe" : `${runnable.length} probes`}
          </Button>
        )}
      </div>
      {state.error && (
        <p role="alert" className="px-4 text-destructive">
          {state.error}
        </p>
      )}
      {state.dismissed.length > 0 && (
        <div className="px-4">
          <p className="font-medium">Dismissed by the probes</p>
          <ul className="mt-0.5 list-disc space-y-1 pl-4 text-muted-foreground">
            {state.dismissed.map((d, i) => (
              <li key={i}>
                <span className="text-foreground">{d.finding.title}</span>: {d.reason}
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}

function ProbeItem({ probe: p, result }: { probe: Probe; result: ProbeResult | undefined }) {
  return (
    <li className={cn("rounded border bg-background/40 px-3 py-2", p.overBudget && "opacity-60")}>
      <p className="font-medium">
        {p.title}
        {p.overBudget && <span className="ml-1 font-normal text-warning">· over budget, not run</span>}
      </p>
      <p className="mt-0.5 leading-relaxed text-muted-foreground">{p.proves}</p>
      <pre aria-label={`SQL of ${p.id}`} className="mt-1 overflow-x-auto rounded bg-muted px-2 py-1 font-mono text-[11px] leading-relaxed">
        {p.sql}
      </pre>
      {(p.params ?? []).length > 0 && (
        <p className="mt-1 font-mono text-[11px] text-muted-foreground">
          {(p.params ?? []).map((prm) => `@${prm.name} = '${prm.value}'`).join(", ")}
        </p>
      )}
      {result && (
        <p className={cn("mt-1", result.error ? "text-destructive" : result.showsProblem ? "text-foreground" : "text-success")}>
          {result.error ? `Failed: ${result.error}` : result.summary}
          <span className="ml-1 text-muted-foreground">({result.elapsedMs} ms)</span>
        </p>
      )}
    </li>
  );
}
