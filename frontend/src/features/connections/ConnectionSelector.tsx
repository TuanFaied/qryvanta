import { AlertTriangle, Check, ChevronDown, Loader2, Plug, Plus, Settings2, Unplug } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import type { ConnectionDTO, ConnectionError } from "@/services/backend";

interface ConnectionSelectorProps {
  connections: ConnectionDTO[];
  activeId: string | null;
  connectingId: string | null;
  connectError: { id: string; error: ConnectionError } | null;
  onConnect: (id: string) => void;
  onDisconnect: () => void;
  onNew: () => void;
  onEdit: (c: ConnectionDTO) => void;
}

/** Header control that shows and switches the active connection. */
export function ConnectionSelector({
  connections,
  activeId,
  connectingId,
  connectError,
  onConnect,
  onDisconnect,
  onNew,
  onEdit,
}: ConnectionSelectorProps) {
  const active = connections.find((c) => c.profile.id === activeId) ?? null;
  const connecting = connections.find((c) => c.profile.id === connectingId) ?? null;
  const failed = connectError ? connections.find((c) => c.profile.id === connectError.id) ?? null : null;

  let label = "No connection";
  if (connecting) label = `Connecting to ${connecting.profile.name}…`;
  else if (active) label = active.profile.name;

  return (
    <div className="flex items-center">
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="ghost" size="sm" className="h-7 max-w-64 gap-1.5 px-2 text-xs font-normal" aria-label="Connection">
            {connecting ? <Loader2 className="size-3.5 animate-spin" /> : <Plug className="size-3.5" />}
            <span
              aria-hidden="true"
              className={cn("size-1.5 shrink-0 rounded-full", active ? "bg-success" : "bg-muted-foreground/50")}
            />
            <span className="truncate">{label}</span>
            <ChevronDown className="size-3 opacity-60" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start" className="w-64">
          <DropdownMenuLabel className="text-xs text-muted-foreground">Connections</DropdownMenuLabel>
          {connections.length === 0 && <p className="px-2 py-1.5 text-xs text-muted-foreground">No saved connections yet.</p>}
          {connections.map((c) => (
            <DropdownMenuItem
              key={c.profile.id}
              className="text-xs"
              disabled={connectingId !== null}
              onSelect={() => c.profile.id !== activeId && onConnect(c.profile.id)}
            >
              <span className="min-w-0 flex-1">
                <span className="block truncate">{c.profile.name}</span>
                <span className="block truncate font-mono text-[10px] text-muted-foreground">{c.profile.server}</span>
              </span>
              {c.profile.id === activeId && <Check className="size-3.5" aria-label="active" />}
            </DropdownMenuItem>
          ))}
          <DropdownMenuSeparator />
          {active && (
            <>
              <DropdownMenuItem className="text-xs" onSelect={() => onEdit(active)}>
                <Settings2 className="size-3.5" /> Edit “{active.profile.name}”…
              </DropdownMenuItem>
              <DropdownMenuItem className="text-xs" onSelect={onDisconnect}>
                <Unplug className="size-3.5" /> Disconnect
              </DropdownMenuItem>
            </>
          )}
          <DropdownMenuItem className="text-xs" onSelect={onNew}>
            <Plus className="size-3.5" /> New connection…
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>

      {connectError && failed && (
        <Tooltip>
          <TooltipTrigger asChild>
            <button
              type="button"
              className="ml-0.5 flex size-6 items-center justify-center rounded-sm text-destructive hover:bg-accent"
              aria-label={`Could not connect to ${failed.profile.name}`}
              onClick={() => onEdit(failed)}
            >
              <AlertTriangle className="size-3.5" />
            </button>
          </TooltipTrigger>
          <TooltipContent side="bottom" className="max-w-80">
            <p className="font-medium">Could not connect to {failed.profile.name}</p>
            <p className="mt-0.5 opacity-80">{connectError.error.message}</p>
            <p className="mt-1 opacity-70">Click to edit the connection.</p>
          </TooltipContent>
        </Tooltip>
      )}
    </div>
  );
}
