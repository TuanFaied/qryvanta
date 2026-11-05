import { ChevronRight } from "lucide-react";
import { useState } from "react";
import { PanelHeader } from "@/components/shell/PanelHeader";
import { ScrollArea } from "@/components/ui/scroll-area";
import { cn } from "@/lib/utils";

const sections = [
  { id: "structure", title: "Query structure", empty: "Sources, joins, filters and grouping of the parsed query." },
  { id: "joins", title: "JOIN analysis", empty: "Join types, predicates, key cardinality and row multiplication." },
  { id: "filters", title: "Filter analysis", empty: "WHERE and HAVING predicates, NULL handling and precedence." },
  { id: "findings", title: "Findings", empty: "Confirmed issues, potential issues and suggestions." },
  { id: "execution", title: "Execution", empty: "Duration, row counts and plan summary of the last run." },
] as const;

type SectionId = (typeof sections)[number]["id"];

export function AnalysisPanel() {
  const [open, setOpen] = useState<Record<SectionId, boolean>>({
    structure: true,
    joins: true,
    filters: true,
    findings: true,
    execution: false,
  });

  return (
    <section aria-label="Analysis" className="flex h-full flex-col bg-panel">
      <PanelHeader title="Analysis" />
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
                </button>
                {isOpen && (
                  <p className="px-7 pt-0.5 pb-3 text-xs leading-relaxed text-muted-foreground">
                    Nothing analysed yet. {s.empty}
                  </p>
                )}
              </div>
            );
          })}
        </div>
      </ScrollArea>
    </section>
  );
}
