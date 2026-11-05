import { ChevronRight, Database, Loader2, Pencil, Plug, PlugZap, Plus, RefreshCw, Search, Server, Trash2, Unplug } from "lucide-react";
import { useMemo, useState, type ReactNode } from "react";
import { Button } from "@/components/ui/button";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";
import { Input } from "@/components/ui/input";
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
  /** Renders the schema tree of a connected server; filter is lower-case. */
  renderSchema?: (connectionId: string, filter: string) => ReactNode;
}

export function ExplorerPanel(props: ExplorerPanelProps) {
  const { connections, activeId, loading, loadError, onRefresh, onNew } = props;
  const [query, setQuery] = useState("");
  const [searching, setSearching] = useState(false);
  // Connection rows the user collapsed. Connected rows are expanded by default.
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({});
  const filter = query.trim().toLowerCase();

  const visible = useMemo(() => {
    if (!filter) return connections;
    // The active connection stays visible while filtering, because its objects are filtered too.
    return connections.filter(
      (c) =>
        c.profile.id === activeId ||
        [c.profile.name, c.profile.server, c.profile.database].some((v) => v.toLowerCase().includes(filter)),
    );
  }, [connections, filter, activeId]);

  return (
    <section aria-label="Database explorer" className="flex h-full flex-col bg-panel">
      <PanelHeader
        title="Explorer"
        actions={
          <>
            <IconButton icon={Search} label="Search" active={searching} onClick={() => setSearching((s) => !s)} />
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
            placeholder="Filter connections and objects"
            aria-label="Filter connections and objects"
            className="h-7 text-xs"
          />
          {filter && activeId && (
            <p className="mt-1 px-0.5 text-[10px] text-muted-foreground">Objects are filtered in databases you have expanded.</p>
          )}
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
        <div className="min-h-0 flex-1 overflow-auto">
          <ul role="tree" aria-label="Connections" className="pb-2">
            {visible.map((c) => {
              const expandable = c.connected && c.profile.id === activeId && !!props.renderSchema;
              const expanded = expandable && !collapsed[c.profile.id];
              return (
                <ConnectionRow
                  key={c.profile.id}
                  connection={c}
                  {...props}
                  expandable={expandable}
                  expanded={expanded}
                  onToggle={() => setCollapsed((s) => ({ ...s, [c.profile.id]: expanded }))}
                >
                  {expanded && <ul role="group">{props.renderSchema?.(c.profile.id, filter)}</ul>}
                </ConnectionRow>
              );
            })}
            {visible.length === 0 && <li className="px-3 py-2 text-xs text-muted-foreground">No connections match “{query}”.</li>}
          </ul>
        </div>
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
  onRefresh,
  expandable,
  expanded,
  onToggle,
  children,
}: ExplorerPanelProps & {
  connection: ConnectionDTO;
  expandable: boolean;
  expanded: boolean;
  onToggle: () => void;
  children?: ReactNode;
}) {
  const p = c.profile;
  const active = p.id === activeId;
  const connecting = p.id === connectingId;
  const target = [p.server + (p.port > 0 ? `,${p.port}` : ""), p.database].filter(Boolean).join(" / ");
  const auth = p.auth === AuthMethod.AuthWindows ? "Windows authentication" : `SQL login ${p.username}`;

  return (
    <li role="none">
      <ContextMenu>
        <ContextMenuTrigger asChild>
          <div
            role="treeitem"
            aria-selected={active}
            aria-expanded={expandable ? expanded : undefined}
            aria-label={p.name}
            tabIndex={0}
            onClick={expandable ? onToggle : undefined}
            onDoubleClick={() => (active ? undefined : onConnect(p.id))}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !active) onConnect(p.id);
              else if (e.key === "ArrowRight" && expandable && !expanded) onToggle();
              else if (e.key === "ArrowLeft" && expandable && expanded) onToggle();
              else if (e.key === "F2") onEdit(c);
              else if (e.key === "Delete") onDelete(c);
            }}
            title={`${target}\n${auth}${p.auth === AuthMethod.AuthSQL && !c.hasPassword ? " (no saved password)" : ""}`}
            className={cn(
              "group mx-1 flex cursor-default items-center gap-1.5 rounded-sm px-1 py-1 outline-none hover:bg-accent/60 focus-visible:ring-1 focus-visible:ring-ring",
              active && "bg-accent",
            )}
          >
            <ChevronRight
              aria-hidden="true"
              className={cn("size-3 shrink-0 text-muted-foreground transition-transform", expanded && "rotate-90", !expandable && "invisible")}
            />
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
          </div>
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
          {active && (
            <ContextMenuItem className="text-xs" onSelect={onRefresh}>
              <RefreshCw className="size-3.5" /> Refresh
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
      {children}
    </li>
  );
}
