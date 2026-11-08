import { AlertTriangle, CircleHelp, Loader2, ShieldAlert, ShieldCheck, UserCheck, UserCog } from "lucide-react";
import { describeAccess } from "@/features/connections/access";
import { cn } from "@/lib/utils";
import type { Config, ParserStatus, ServerInfo, WriteAccess } from "@/services/backend";

interface StatusBarProps {
  config: Config;
  onOpenSettings: () => void;
  /** The active connection, if any. */
  connection: { name: string; server: ServerInfo } | null;
  /** Write access of the active login in the current database; undefined while checking. */
  access?: WriteAccess | undefined;
  onRecheckAccess?: () => void;
  /** The T-SQL parser; a warning is shown only when it is unavailable. */
  parser?: ParserStatus | null;
  onRecheckParser?: () => void;
}

function shortVersion(s: ServerInfo): string {
  // "16.0.4305.5" -> "SQL Server 16.0"; Azure reports its own version line.
  const [major, minor] = s.productVersion.split(".");
  if (s.cloud) return "Azure SQL";
  return major ? `SQL Server ${major}.${minor ?? "0"}` : "SQL Server";
}

const fmt = new Intl.NumberFormat();

export function StatusBar({ config, onOpenSettings, connection, access, onRecheckAccess, parser, onRecheckParser }: StatusBarProps) {
  const { readOnly, timeoutSeconds, maxRows } = config.execution;
  return (
    <footer className="flex h-6 shrink-0 items-center gap-3 border-t bg-chrome px-2 text-[11px] text-muted-foreground">
      <button
        type="button"
        onClick={onOpenSettings}
        className="flex shrink-0 items-center gap-1 rounded-sm px-1 hover:bg-accent hover:text-foreground"
        title="Qryvanta's execution policy. Click to change."
      >
        {readOnly ? (
          <>
            <ShieldCheck className="size-3.5 text-success" /> Read-only mode
          </>
        ) : (
          <>
            <ShieldAlert className="size-3.5 text-warning" /> Writes allowed
          </>
        )}
      </button>
      {connection ? (
        <>
          <AccessIndicator access={access} onRecheck={onRecheckAccess} />
          <span className="truncate" title={`${connection.server.serverName} · ${connection.server.edition}`}>
            {connection.name} · {shortVersion(connection.server)} · {connection.server.login} · {connection.server.database}
          </span>
        </>
      ) : (
        <span>No connection</span>
      )}
      <div className="flex-1" />
      {parser && !parser.available && (
        <button
          type="button"
          onClick={onRecheckParser}
          className="flex shrink-0 items-center gap-1 rounded-sm px-1 text-warning hover:bg-accent"
          title={`The T-SQL parser is not available: ${parser.reason}\n\nQueries still run; syntax checks and analysis need the parser. Click to try again.`}
        >
          <AlertTriangle className="size-3.5" /> Parser unavailable
        </button>
      )}
      <button
        type="button"
        onClick={onOpenSettings}
        className="shrink-0 rounded-sm px-1 hover:bg-accent hover:text-foreground"
        title="Default execution limits. Click to change."
      >
        Timeout {timeoutSeconds}s · Max {fmt.format(maxRows)} rows
      </button>
    </footer>
  );
}

/** What the connected login could change, from the server's permission check. */
export function AccessIndicator({ access, onRecheck }: { access: WriteAccess | undefined; onRecheck?: (() => void) | undefined }) {
  const d = describeAccess(access);
  const Icon = !access ? Loader2 : d.tone === "ok" ? UserCheck : d.tone === "warn" ? UserCog : d.tone === "danger" ? AlertTriangle : CircleHelp;
  const reasons = (access?.reasons ?? []).map((r) => `• ${r.text}`).join("\n") ?? "";
  const title = [d.detail, reasons, onRecheck ? "Click to check again." : ""].filter(Boolean).join("\n\n");
  return (
    <button
      type="button"
      onClick={onRecheck}
      title={title}
      aria-label={`${d.label}. ${d.detail}`}
      className={cn(
        "flex shrink-0 items-center gap-1 rounded-sm px-1 hover:bg-accent",
        d.tone === "ok" && "text-success",
        d.tone === "warn" && "text-warning",
        d.tone === "danger" && "bg-destructive/15 text-destructive",
      )}
    >
      <Icon className={cn("size-3.5", !access && "animate-spin")} aria-hidden="true" />
      {d.label}
    </button>
  );
}
