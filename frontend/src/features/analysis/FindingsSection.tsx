import { Classification, EvidenceKind, Severity, type Analysis, type Evidence, type Finding } from "@/services/backend";
import { editorPosition, type AnalysisTarget } from "@/stores/analysis-store";
import { cn } from "@/lib/utils";

const severityOrder: Severity[] = [
  Severity.SeverityCritical,
  Severity.SeverityHigh,
  Severity.SeverityMedium,
  Severity.SeverityLow,
  Severity.SeverityInformational,
];

const severityLabel: Record<string, string> = {
  [Severity.SeverityCritical]: "Critical",
  [Severity.SeverityHigh]: "High",
  [Severity.SeverityMedium]: "Medium",
  [Severity.SeverityLow]: "Low",
  [Severity.SeverityInformational]: "Informational",
};

const severityClass: Record<string, string> = {
  [Severity.SeverityCritical]: "text-destructive",
  [Severity.SeverityHigh]: "text-destructive",
  [Severity.SeverityMedium]: "text-warning",
  [Severity.SeverityLow]: "text-info",
  [Severity.SeverityInformational]: "text-muted-foreground",
};

export const classificationLabel: Record<string, string> = {
  [Classification.Confirmed]: "Confirmed issue",
  [Classification.Potential]: "Potential issue",
  [Classification.Optimization]: "Optimization suggestion",
};

const evidenceLabel: Record<string, string> = {
  [EvidenceKind.EvidenceStatic]: "Query",
  [EvidenceKind.EvidenceMetadata]: "Metadata",
  [EvidenceKind.EvidencePlan]: "Plan",
  [EvidenceKind.EvidenceProbe]: "Probe",
};

export interface FindingsSectionProps {
  target: AnalysisTarget | null;
  running: boolean;
  result: Analysis | null;
  error: string | null;
  /** The active tab, to tell whether the analysis belongs to it and is current. */
  activeTab: { id: string; content: string } | null;
  onRevealRange: (line: number, column: number, length: number) => void;
}

const note = "px-7 pt-0.5 pb-3 text-xs leading-relaxed text-muted-foreground";

/** The Findings section of the analysis panel. */
export function FindingsSection({ target, running, result, error, activeTab, onRevealRange }: FindingsSectionProps) {
  if (running) return <p className={note}>Analyzing…</p>;
  if (error) return <p className={cn(note, "text-destructive")}>Could not analyze the query: {error}</p>;
  if (!target || !result) {
    return (
      <p className={note}>
        Nothing analyzed yet. Analyze runs the diagnostic rules on the selection, or on the whole tab. It needs no connection
        and runs nothing.
      </p>
    );
  }
  if (!result.available) {
    return (
      <p role="status" className={cn(note, "text-warning")}>
        Parser unavailable, so the query could not be analyzed{result.reason ? `: ${result.reason}` : "."}
      </p>
    );
  }

  const otherTab = activeTab?.id !== target.tabId;
  const changed = !otherTab && activeTab?.content !== target.tabText;
  const findings = result.findings ?? [];
  const failures = result.failures ?? [];

  return (
    <div className="space-y-2 px-3 pt-0.5 pb-3 text-xs">
      {otherTab && <p className="px-4 text-muted-foreground">These findings are for another tab.</p>}
      {changed && <p className="px-4 text-warning">The text changed after this analysis. Analyze again for current positions.</p>}
      {result.syntaxErrors > 0 && (
        <p className="px-4 text-warning">
          {result.syntaxErrors === 1 ? "1 syntax error" : `${result.syntaxErrors} syntax errors`}: batches with syntax errors were
          not analyzed.
        </p>
      )}
      {findings.length === 0 ? (
        <p role="status" className="px-4 text-muted-foreground">
          {result.statements === 0
            ? "Nothing to analyze: no statements were found."
            : `No findings. ${result.rules === 1 ? "1 rule" : `${result.rules} rules`} found nothing to report.`}
        </p>
      ) : (
        severityOrder.map((sev) => {
          const group = findings.filter((f) => f.severity === sev);
          if (group.length === 0) return null;
          return (
            <section key={sev} aria-label={`${severityLabel[sev]} findings`}>
              <h3 className={cn("px-4 pb-1 text-[11px] font-semibold", severityClass[sev])}>
                {severityLabel[sev]} · {group.length}
              </h3>
              <ul className="space-y-2">
                {group.map((f, i) => (
                  <FindingItem key={`${f.ruleId}-${f.span.offset}-${i}`} finding={f} target={target} canReveal={!otherTab} onRevealRange={onRevealRange} />
                ))}
              </ul>
            </section>
          );
        })
      )}
      {failures.length > 0 && (
        <div role="alert" className="px-4 text-destructive">
          <p>Some rules failed, so findings may be missing:</p>
          <ul className="list-disc pl-4">
            {failures.map((f, i) => (
              <li key={i}>
                <span className="font-mono">{f.ruleId}</span>: {f.message}
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}

function FindingItem({
  finding: f,
  target,
  canReveal,
  onRevealRange,
}: {
  finding: Finding;
  target: AnalysisTarget;
  canReveal: boolean;
  onRevealRange: (line: number, column: number, length: number) => void;
}) {
  const at = editorPosition(target, f.span.line, f.span.column);
  return (
    <li className="rounded border bg-background/40 px-3 py-2">
      <button
        type="button"
        disabled={!canReveal}
        title={canReveal ? "Select it in the editor" : "Switch to the analyzed tab to select it"}
        onClick={() => onRevealRange(at.line, at.column, f.span.length)}
        className="text-left font-medium underline-offset-2 enabled:hover:underline focus-visible:outline-1"
      >
        {f.title}
      </button>
      <p className="mt-0.5 text-[11px] text-muted-foreground">
        <span className={f.classification === Classification.Confirmed ? "text-foreground" : undefined}>
          {classificationLabel[f.classification] ?? f.classification}
        </span>
        {" · "}
        <span className="font-mono">{f.ruleId}</span>
        {" · "}Line {at.line}, column {at.column}
      </p>
      <p className="mt-1 leading-relaxed whitespace-pre-line">{f.explanation}</p>
      {(f.evidence ?? []).length > 0 && (
        <div className="mt-1.5">
          <p className="text-[11px] font-medium text-muted-foreground">Evidence</p>
          <ul className="mt-0.5 space-y-1">
            {(f.evidence ?? []).map((e, i) => (
              <EvidenceItem key={i} evidence={e} />
            ))}
          </ul>
        </div>
      )}
      {f.correction && (
        <div className="mt-1.5" aria-label="Suggested correction">
          <p className="text-[11px] font-medium text-muted-foreground">
            Suggested correction · not applied automatically
          </p>
          <p className="mt-0.5">{f.correction.description}</p>
          <pre className="mt-1 overflow-x-auto rounded bg-muted px-2 py-1 font-mono text-[11px] leading-relaxed">
            <del className="block text-destructive no-underline">
              - {target.sql.slice(f.correction.span.offset, f.correction.span.offset + f.correction.span.length)}
            </del>
            <ins className="block text-success no-underline">+ {f.correction.replacement}</ins>
          </pre>
        </div>
      )}
    </li>
  );
}

function EvidenceItem({ evidence: e }: { evidence: Evidence }) {
  return (
    <li>
      <span className="text-[11px] text-muted-foreground">{evidenceLabel[e.kind] ?? e.kind}: </span>
      {e.summary}
      {e.static && <code className="mt-0.5 block rounded bg-muted px-1.5 py-0.5 font-mono text-[11px]">{e.static.text}</code>}
      {e.metadata && (
        <code className="mt-0.5 block font-mono text-[11px]">
          {e.metadata.object}
          {e.metadata.column ? `.${e.metadata.column}` : ""}: {e.metadata.property} = {e.metadata.value}
        </code>
      )}
      {e.plan && (
        <code className="mt-0.5 block font-mono text-[11px]">
          {e.plan.operator} (node {e.plan.nodeId}): {e.plan.property} = {e.plan.value} · {e.plan.actual ? "actual" : "estimated"}
        </code>
      )}
      {e.probe && (
        <>
          <pre className="mt-0.5 overflow-x-auto rounded bg-muted px-1.5 py-0.5 font-mono text-[11px]">{e.probe.sql}</pre>
          <span className="text-[11px]">Result: {e.probe.result}</span>
        </>
      )}
    </li>
  );
}
