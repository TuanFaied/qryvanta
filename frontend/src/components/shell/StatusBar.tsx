import { ShieldAlert, ShieldCheck } from "lucide-react";
import type { Config } from "@/services/backend";

interface StatusBarProps {
  config: Config;
  onOpenSettings: () => void;
}

const fmt = new Intl.NumberFormat();

export function StatusBar({ config, onOpenSettings }: StatusBarProps) {
  const { readOnly, timeoutSeconds, maxRows } = config.execution;
  return (
    <footer className="flex h-6 shrink-0 items-center gap-3 border-t bg-chrome px-2 text-[11px] text-muted-foreground">
      <button
        type="button"
        onClick={onOpenSettings}
        className="flex items-center gap-1 rounded-sm px-1 hover:bg-accent hover:text-foreground"
        title="Execution policy. Click to change."
      >
        {readOnly ? (
          <>
            <ShieldCheck className="size-3.5 text-success" /> Read-only
          </>
        ) : (
          <>
            <ShieldAlert className="size-3.5 text-warning" /> Writes allowed
          </>
        )}
      </button>
      <span>No connection</span>
      <div className="flex-1" />
      <button
        type="button"
        onClick={onOpenSettings}
        className="rounded-sm px-1 hover:bg-accent hover:text-foreground"
        title="Default execution limits. Click to change."
      >
        Timeout {timeoutSeconds}s · Max {fmt.format(maxRows)} rows
      </button>
    </footer>
  );
}
