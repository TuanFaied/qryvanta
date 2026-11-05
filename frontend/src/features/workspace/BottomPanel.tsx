import { BarChart3, GitFork, ListChecks, MessageSquareText, Table2 } from "lucide-react";
import { EmptyState } from "@/components/shell/EmptyState";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";

const tabs = [
  { value: "results", label: "Results", icon: Table2, title: "No results", body: "Rows returned by the query appear here." },
  {
    value: "messages",
    label: "Messages",
    icon: MessageSquareText,
    title: "No messages",
    body: "Server messages, row counts and errors appear here.",
  },
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

export function BottomPanel() {
  return (
    <section aria-label="Output" className="flex h-full flex-col bg-panel">
      <Tabs defaultValue="results" className="flex h-full flex-col gap-0">
        <TabsList className="h-8 w-full justify-start gap-0 rounded-none border-b bg-transparent p-0 px-1">
          {tabs.map((t) => (
            <TabsTrigger
              key={t.value}
              value={t.value}
              className="h-8 flex-none rounded-none border-0 border-b-2 border-transparent px-3 text-xs font-normal text-muted-foreground shadow-none data-[state=active]:border-b-primary data-[state=active]:bg-transparent data-[state=active]:text-foreground data-[state=active]:shadow-none dark:data-[state=active]:border-b-primary dark:data-[state=active]:bg-transparent"
            >
              {t.label}
            </TabsTrigger>
          ))}
        </TabsList>
        {tabs.map((t) => (
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
