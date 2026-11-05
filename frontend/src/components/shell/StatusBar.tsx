import { ShieldAlert, ShieldCheck } from "lucide-react";
import type { Config, ServerInfo } from "@/services/backend";

interface StatusBarProps {
  config: Config;
  onOpenSettings: () => void;
  /** The active connection, if any. */
  connection: { name: string; server: ServerInfo } | null;
}

function shortVersion(s: ServerInfo): string {
  // "16.0.4305.5" -> "SQL Server 16.0"; Azure reports its own version line.
  const [major, minor] = s.productVersion.split(".");
  if (s.cloud) return "Azure SQL";
  return major ? `SQL Server ${major}.${minor ?? "0"}` : "SQL Server";
}

const fmt = new Intl.NumberFormat();

export function StatusBar({ config, onOpenSettings, connection }: StatusBarProps) {
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
      {connection ? (
        <span className="truncate" title={`${connection.server.serverName} · ${connection.server.edition}`}>
          {connection.name} · {shortVersion(connection.server)} · {connection.server.login} · {connection.server.database}
        </span>
      ) : (
        <span>No connection</span>
      )}
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
