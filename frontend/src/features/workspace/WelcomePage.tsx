import { AlertTriangle, Bug, FileCode2, Plug } from "lucide-react";
import { LogoMark } from "@/components/brand/Logo";

const steps = [
  {
    icon: Plug,
    title: "Connect to SQL Server",
    body: "Save a connection. Passwords are kept in Windows Credential Manager, never in config files.",
    phase: "Phase 2",
  },
  {
    icon: FileCode2,
    title: "Write and run a query",
    body: "Execute in read-only mode with time, row and size limits you control.",
    phase: "Phase 3",
  },
  {
    icon: Bug,
    title: "Execute with debugging",
    body: "See where rows were dropped or multiplied, with evidence from the query, plan and approved probe queries.",
    phase: "Phase 5",
  },
];

const shortcuts: [string, string][] = [
  ["Execute", "Ctrl+Enter"],
  ["Execute with debugging", "Ctrl+Shift+Enter"],
  ["Cancel execution", "Esc"],
  ["Save query", "Ctrl+S"],
];

export function WelcomePage({ warnings }: { warnings: string[] }) {
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
                  <span className="rounded-sm bg-muted px-1.5 py-px text-[10px] text-muted-foreground">{s.phase}</span>
                </div>
                <p className="mt-0.5 text-xs leading-relaxed text-muted-foreground">{s.body}</p>
              </div>
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
