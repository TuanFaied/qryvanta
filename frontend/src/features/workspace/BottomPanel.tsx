import { BarChart3, GitFork, ListChecks } from "lucide-react";
import { EmptyState } from "@/components/shell/EmptyState";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { MessagesView } from "@/features/execution/MessagesView";
import { ResultsView } from "@/features/execution/ResultsView";
import { RunStatus } from "@/features/execution/RunStatus";
import type { Run } from "@/stores/execution-store";

export type OutputTab = "results" | "messages" | "diagnostics" | "plan" | "stats";

const placeholders = [
  {
    value: "diagnostics",
    label: "Diagnostics",
    icon: ListChecks,
    title: "No diagnostics",
    body: "Findings from Execute with debugging appear here, each with its evidence and severity.",
  },
  {
    value: "plan",
    label: "Execution plan",
    icon: GitFork,
    title: "No execution plan",
    body: "The estimated or actual plan appears here. Estimated and measured values are always labelled.",
  },
  {
    value: "stats",
    label: "Statistics",
    icon: BarChart3,
    title: "No statistics",
    body: "Duration, rows and I/O measured during execution appear here.",
  },
] as const;

const triggerClass =
  "h-8 flex-none rounded-none border-0 border-b-2 border-transparent px-3 text-xs font-normal text-muted-foreground shadow-none data-[state=active]:border-b-primary data-[state=active]:bg-transparent data-[state=active]:text-foreground data-[state=active]:shadow-none dark:data-[state=active]:border-b-primary dark:data-[state=active]:bg-transparent";

interface BottomPanelProps {
  /** The active editor tab's latest run. */
  run: Run | null;
  tab: OutputTab;
  onTabChange: (tab: OutputTab) => void;
  maxRows: number;
  timeoutSeconds: number;
  onRevealLine: (line: number) => void;
}

export function BottomPanel({ run, tab, onTabChange, maxRows, timeoutSeconds, onRevealLine }: BottomPanelProps) {
  const errors = run?.messages.filter((m) => m.kind === "error").length ?? 0;
  return (
    <section aria-label="Output" className="flex h-full flex-col bg-panel">
      <Tabs value={tab} onValueChange={(v) => onTabChange(v as OutputTab)} className="flex h-full flex-col gap-0">
        <div className="flex h-8 shrink-0 items-center border-b pr-3">
          <TabsList className="h-8 shrink-0 justify-start gap-0 rounded-none bg-transparent p-0 px-1">
            <TabsTrigger value="results" className={triggerClass}>
              Results
            </TabsTrigger>
            <TabsTrigger value="messages" className={triggerClass}>
              Messages
              {errors > 0 && (
                <span className="ml-1.5 rounded-sm bg-destructive/15 px-1 text-[10px] text-destructive" aria-label={`${errors} errors`}>
                  {errors}
                </span>
              )}
            </TabsTrigger>
            {placeholders.map((t) => (
              <TabsTrigger key={t.value} value={t.value} className={triggerClass}>
                {t.label}
              </TabsTrigger>
            ))}
          </TabsList>
          <div className="ml-auto flex min-w-0 pl-2">{run && <RunStatus run={run} />}</div>
        </div>
        <TabsContent value="results" className="min-h-0 flex-1">
          <ResultsView run={run} maxRows={maxRows} />
        </TabsContent>
        <TabsContent value="messages" className="min-h-0 flex-1">
          <MessagesView run={run} timeoutSeconds={timeoutSeconds} onRevealLine={onRevealLine} />
        </TabsContent>
        {placeholders.map((t) => (
          <TabsContent key={t.value} value={t.value} className="min-h-0 flex-1">
            <EmptyState icon={t.icon} title={t.title}>
              {t.body}
            </EmptyState>
          </TabsContent>
        ))}
      </Tabs>
    </section>
  );
}
