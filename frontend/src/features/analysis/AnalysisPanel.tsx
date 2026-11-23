import { ChevronRight, ScanSearch } from "lucide-react";
import { useState, type ReactNode } from "react";
import { PanelHeader } from "@/components/shell/PanelHeader";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { cn } from "@/lib/utils";
import { useAnalysisStore } from "@/stores/analysis-store";
import { FindingsSection } from "./FindingsSection";
import { ProbesSection } from "./ProbesSection";

const sections = [
  { id: "findings", title: "Findings", empty: "" },
  { id: "probes", title: "Probes", empty: "" },
  { id: "structure", title: "Query structure", empty: "Sources, joins, filters and grouping of the parsed query." },
  { id: "joins", title: "JOIN analysis", empty: "Join types, predicates, key cardinality and row multiplication." },
  { id: "filters", title: "Filter analysis", empty: "WHERE and HAVING predicates, NULL handling and precedence." },
  { id: "execution", title: "Execution", empty: "Duration, row counts and plan summary of the last run." },
] as const;

type SectionId = (typeof sections)[number]["id"];

export interface AnalysisPanelProps {
  /** The active editor tab, or null when none is open. */
  activeTab: { id: string; content: string } | null;
  /** Analyzes the selection or the whole active tab. */
  onAnalyze: () => void;
  /** Selects a range of the active tab in the editor. */
  onRevealRange: (line: number, column: number, length: number) => void;
}

export function AnalysisPanel({ activeTab, onAnalyze, onRevealRange }: AnalysisPanelProps) {
  const [open, setOpen] = useState<Record<SectionId, boolean>>({
    findings: true,
    probes: true,
    structure: true,
    joins: true,
    filters: true,
    execution: false,
  });
  const { target, running, result, error, probes, runProbes, cancelProbes, steps } = useAnalysisStore();
  const count = result?.available ? (result.findings ?? []).length : 0;

  const body: Partial<Record<SectionId, ReactNode>> = {
    findings: (
      <FindingsSection
        target={target}
        running={running}
        result={result}
        error={error}
        activeTab={activeTab}
        onRevealRange={onRevealRange}
        steps={steps}
      />
    ),
    probes: (
      <ProbesSection
        result={running ? null : result}
        probes={probes}
        current={!!target && activeTab?.id === target.tabId}
        onRun={(ids) => void runProbes(ids)}
        onCancel={cancelProbes}
      />
    ),
  };

  return (
    <section aria-label="Analysis" className="flex h-full flex-col bg-panel">
      <PanelHeader
        title="Analysis"
        actions={
          <Button
            size="sm"
            variant="ghost"
            className="h-6 gap-1 px-2 text-xs"
            disabled={!activeTab || running}
            onClick={onAnalyze}
            title="Run the diagnostic rules on the selection or the whole tab"
          >
            <ScanSearch className="size-3.5" />
            Analyze
          </Button>
        }
      />
      <ScrollArea className="min-h-0 flex-1">
        <div className="pb-3">
          {sections.map((s) => {
            const isOpen = open[s.id];
            return (
              <div key={s.id} className="border-t first:border-t-0">
                <button
                  type="button"
                  aria-expanded={isOpen}
                  onClick={() => setOpen((o) => ({ ...o, [s.id]: !o[s.id] }))}
                  className="flex h-7 w-full items-center gap-1 px-2 text-left text-xs font-medium hover:bg-accent/50 focus-visible:outline-1"
                >
                  <ChevronRight className={cn("size-3.5 text-muted-foreground transition-transform", isOpen && "rotate-90")} />
                  {s.title}
                  {s.id === "findings" && count > 0 && <span className="ml-1 text-muted-foreground">({count})</span>}
                </button>
                {isOpen &&
                  (body[s.id] ?? (
                    <p className="px-7 pt-0.5 pb-3 text-xs leading-relaxed text-muted-foreground">Nothing analyzed yet. {s.empty}</p>
                  ))}
              </div>
            );
          })}
        </div>
      </ScrollArea>
    </section>
  );
}
