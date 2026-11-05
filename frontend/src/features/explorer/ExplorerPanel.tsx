import { Database, Loader2, Pencil, Plug, PlugZap, Plus, RefreshCw, Search, Server, Trash2, Unplug } from "lucide-react";
import { useMemo, useState } from "react";
import { Button } from "@/components/ui/button";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";
import { Input } from "@/components/ui/input";
import { ScrollArea } from "@/components/ui/scroll-area";
import { EmptyState } from "@/components/shell/EmptyState";
import { IconButton } from "@/components/shell/IconButton";
import { PanelHeader } from "@/components/shell/PanelHeader";
import { cn } from "@/lib/utils";
import { AuthMethod, type ConnectionDTO } from "@/services/backend";

export interface ExplorerPanelProps {
  connections: ConnectionDTO[];
  activeId: string | null;
  connectingId: string | null;
  loading: boolean;
  loadError: string | null;
  onRefresh: () => void;
  onNew: () => void;
  onEdit: (c: ConnectionDTO) => void;
  onDelete: (c: ConnectionDTO) => void;
  onConnect: (id: string) => void;
  onDisconnect: () => void;
}

export function ExplorerPanel(props: ExplorerPanelProps) {
  const { connections, loading, loadError, onRefresh, onNew } = props;
  const [query, setQuery] = useState("");
  const [searching, setSearching] = useState(false);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return connections;
    return connections.filter((c) =>
      [c.profile.name, c.profile.server, c.profile.database].some((v) => v.toLowerCase().includes(q)),
    );
  }, [connections, query]);

  return (
    <section aria-label="Database explorer" className="flex h-full flex-col bg-panel">
      <PanelHeader
        title="Explorer"
        actions={
          <>
            <IconButton icon={Search} label="Search connections" active={searching} onClick={() => setSearching((s) => !s)} />
            <IconButton icon={RefreshCw} label="Refresh" onClick={onRefresh} disabled={loading} />
            <IconButton icon={Plus} label="New connection" onClick={onNew} />
          </>
        }
      />
      {searching && (
        <div className="px-2 pb-2">
          <Input
            autoFocus
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={(e) => e.key === "Escape" && (setQuery(""), setSearching(false))}
            placeholder="Filter by name, server or database"
            aria-label="Filter connections"
            className="h-7 text-xs"
          />
        </div>
      )}

      {loadError ? (
        <EmptyState icon={Server} title="Could not load connections">
          <p className="selectable">{loadError}</p>
          <Button size="sm" variant="outline" className="mt-3 h-7 text-xs" onClick={onRefresh}>
            Try again
          </Button>
        </EmptyState>
      ) : connections.length === 0 ? (
        <EmptyState icon={Server} title="No connections">
          <p>Add a SQL Server connection to browse its databases and debug queries.</p>
          <Button size="sm" className="mt-3 h-7 text-xs" onClick={onNew}>
            <Plus className="size-3.5" /> New connection
          </Button>
        </EmptyState>
      ) : (
        <ScrollArea className="min-h-0 flex-1">
          <ul role="tree" aria-label="Connections" className="pb-2">
            {filtered.map((c) => (
              <ConnectionRow key={c.profile.id} connection={c} {...props} />
            ))}
            {filtered.length === 0 && <li className="px-3 py-2 text-xs text-muted-foreground">No connections match “{query}”.</li>}
          </ul>
          <p className="px-3 pt-1 text-[11px] leading-relaxed text-muted-foreground/80">
            Databases, schemas and tables appear here once the schema explorer lands (#9).
          </p>
        </ScrollArea>
      )}
    </section>
  );
}

function ConnectionRow({
  connection: c,
  activeId,
  connectingId,
  onEdit,
  onDelete,
  onConnect,
  onDisconnect,
}: ExplorerPanelProps & { connection: ConnectionDTO }) {
  const p = c.profile;
  const active = p.id === activeId;
  const connecting = p.id === connectingId;
  const target = [p.server + (p.port > 0 ? `,${p.port}` : ""), p.database].filter(Boolean).join(" / ");
  const auth = p.auth === AuthMethod.AuthWindows ? "Windows authentication" : `SQL login ${p.username}`;

  return (
    <ContextMenu>
      <ContextMenuTrigger asChild>
        <li
          role="treeitem"
          aria-selected={active}
          aria-label={p.name}
          tabIndex={0}
          onDoubleClick={() => (active ? undefined : onConnect(p.id))}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !active) onConnect(p.id);
            if (e.key === "F2") onEdit(c);
            if (e.key === "Delete") onDelete(c);
          }}
          title={`${target}\n${auth}${p.auth === AuthMethod.AuthSQL && !c.hasPassword ? " (no saved password)" : ""}`}
          className={cn(
            "group mx-1 flex cursor-default items-center gap-2 rounded-sm px-2 py-1 outline-none hover:bg-accent/60 focus-visible:ring-1 focus-visible:ring-ring",
            active && "bg-accent",
          )}
        >
          <span className="relative">
            <Database className="size-4 text-muted-foreground" strokeWidth={1.75} />
            <span
              aria-hidden="true"
              className={cn(
                "absolute -right-0.5 -bottom-0.5 size-2 rounded-full border border-panel",
                c.connected ? "bg-success" : "bg-muted-foreground/40",
              )}
            />
          </span>
          <span className="min-w-0 flex-1">
            <span className="block truncate text-xs font-medium">{p.name}</span>
            <span className="block truncate font-mono text-[10px] text-muted-foreground">{target}</span>
          </span>
          {connecting && <Loader2 className="size-3.5 animate-spin text-muted-foreground" aria-label="Connecting" />}
          <span className="sr-only">{c.connected ? "connected" : "not connected"}</span>
        </li>
      </ContextMenuTrigger>
      <ContextMenuContent className="w-48">
        {active ? (
          <ContextMenuItem className="text-xs" onSelect={onDisconnect}>
            <Unplug className="size-3.5" /> Disconnect
          </ContextMenuItem>
        ) : (
          <ContextMenuItem className="text-xs" disabled={connecting} onSelect={() => onConnect(p.id)}>
            {c.connected ? <PlugZap className="size-3.5" /> : <Plug className="size-3.5" />} Connect
          </ContextMenuItem>
        )}
        <ContextMenuItem className="text-xs" onSelect={() => onEdit(c)}>
          <Pencil className="size-3.5" /> Edit… <span className="ml-auto text-muted-foreground">F2</span>
        </ContextMenuItem>
        <ContextMenuSeparator />
        <ContextMenuItem variant="destructive" className="text-xs" onSelect={() => onDelete(c)}>
          <Trash2 className="size-3.5" /> Delete… <span className="ml-auto">Del</span>
        </ContextMenuItem>
      </ContextMenuContent>
    </ContextMenu>
  );
}
