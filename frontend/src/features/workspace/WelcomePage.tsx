import { AlertTriangle, Bug, Check, FileCode2, Plug } from "lucide-react";
import { LogoMark } from "@/components/brand/Logo";
import { Button } from "@/components/ui/button";

const steps = [
  {
    icon: Plug,
    title: "Connect to SQL Server",
    body: "Save a connection. Passwords are kept in Windows Credential Manager, never in config files.",
    phase: "",
  },
  {
    icon: FileCode2,
    title: "Write and run a query",
    body: "Open a .sql file or start a new query. Execution in read-only mode, with time, row and size limits you control, comes next.",
    phase: "",
  },
  {
    icon: Bug,
    title: "Execute with debugging",
    body: "See where rows were dropped or multiplied, with evidence from the query, plan and approved probe queries.",
    phase: "Phase 5",
  },
];

const shortcuts: [string, string][] = [
  ["New query", "Ctrl+N"],
  ["Open file", "Ctrl+O"],
  ["Format document", "Shift+Alt+F"],
  ["Execute", "Ctrl+Enter"],
  ["Execute with debugging", "Ctrl+Shift+Enter"],
  ["Cancel execution", "Esc"],
  ["Save query", "Ctrl+S"],
];

interface WelcomePageProps {
  warnings: string[];
  /** Name of the active connection, if any. */
  connectedTo: string | null;
  onNewConnection: () => void;
  onNewQuery: () => void;
  onOpenFile: () => void;
}

export function WelcomePage({ warnings, connectedTo, onNewConnection, onNewQuery, onOpenFile }: WelcomePageProps) {
  return (
    <div className="h-full overflow-auto bg-editor">
      <div className="mx-auto flex max-w-2xl flex-col gap-8 px-8 py-12">
        {warnings.length > 0 && (
          <div role="alert" className="flex gap-3 rounded-md border border-warning/40 bg-warning/10 p-3 text-xs">
            <AlertTriangle className="mt-0.5 size-4 shrink-0 text-warning" />
            <ul className="selectable space-y-1">
              {warnings.map((w) => (
                <li key={w}>{w}</li>
              ))}
            </ul>
          </div>
        )}

        <div className="flex items-center gap-3">
          <LogoMark className="size-9" />
          <div>
            <h1 className="text-xl font-semibold tracking-tight">Qryvanta</h1>
            <p className="text-sm text-muted-foreground">Understand why a query returned the rows it did.</p>
          </div>
        </div>

        <ol className="grid gap-2">
          {steps.map((s, i) => (
            <li key={s.title} className="flex gap-3 rounded-md border bg-card/40 p-3">
              <div className="flex size-7 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground">
                <s.icon className="size-4" strokeWidth={1.75} />
              </div>
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <span className="text-[13px] font-medium">
                    {i + 1}. {s.title}
                  </span>
                  {s.phase && (
                    <span className="rounded-sm bg-muted px-1.5 py-px text-[10px] text-muted-foreground">{s.phase}</span>
                  )}
                </div>
                <p className="mt-0.5 text-xs leading-relaxed text-muted-foreground">{s.body}</p>
              </div>
              {i === 0 &&
                (connectedTo ? (
                  <span className="flex shrink-0 items-center gap-1 self-center text-xs text-success">
                    <Check className="size-3.5" /> Connected to {connectedTo}
                  </span>
                ) : (
                  <Button size="sm" className="h-7 shrink-0 self-center text-xs" onClick={onNewConnection}>
                    New connection
                  </Button>
                ))}
              {i === 1 && (
                <div className="flex shrink-0 gap-1.5 self-center">
                  <Button size="sm" variant="outline" className="h-7 text-xs" onClick={onOpenFile}>
                    Open file
                  </Button>
                  <Button size="sm" className="h-7 text-xs" onClick={onNewQuery}>
                    New query
                  </Button>
                </div>
              )}
            </li>
          ))}
        </ol>

        <div>
          <h2 className="mb-2 text-[11px] font-semibold tracking-wider text-muted-foreground uppercase">Shortcuts</h2>
          <dl className="grid grid-cols-[1fr_auto] gap-x-6 gap-y-1.5 text-xs">
            {shortcuts.map(([label, keys]) => (
              <div key={label} className="contents">
                <dt className="text-muted-foreground">{label}</dt>
                <dd>
                  <kbd className="rounded-sm border bg-muted px-1.5 py-px font-mono text-[11px]">{keys}</kbd>
                </dd>
              </div>
            ))}
          </dl>
        </div>
      </div>
    </div>
  );
}
