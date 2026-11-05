import { Check, ChevronDown, Database as DatabaseIcon, Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import type { Database } from "@/services/backend";
import type { Load } from "@/stores/schema-store";

interface DatabaseSelectorProps {
  /** Null when no connection is active. */
  databases: Load<Database[]> | undefined | null;
  current: string | null;
  onOpen: () => void;
  onSelect: (name: string) => void;
}

/** Header control for the current database of the active connection. */
export function DatabaseSelector({ databases, current, onOpen, onSelect }: DatabaseSelectorProps) {
  const disabled = databases === null;
  const list = databases?.data ?? [];
  const user = list.filter((d) => d.accessible && !d.system);
  const system = list.filter((d) => d.accessible && d.system);

  const item = (d: Database) => (
    <DropdownMenuItem key={d.name} className="text-xs" onSelect={() => onSelect(d.name)}>
      <span className="min-w-0 flex-1 truncate">{d.name}</span>
      {d.readOnly && <span className="text-[10px] text-muted-foreground">read-only</span>}
      {d.name === current && <Check className="size-3.5" aria-label="current" />}
    </DropdownMenuItem>
  );

  return (
    <DropdownMenu onOpenChange={(open) => open && onOpen()}>
      <DropdownMenuTrigger asChild>
        <Button
          variant="ghost"
          size="sm"
          disabled={disabled}
          className="h-7 max-w-56 gap-1.5 px-2 text-xs font-normal"
          aria-label="Database"
          title={disabled ? "Connect to a server first" : "Current database"}
        >
          <DatabaseIcon className="size-3.5" />
          <span className="truncate">{current ?? "No database"}</span>
          <ChevronDown className="size-3 opacity-60" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="max-h-80 w-56 overflow-y-auto">
        <DropdownMenuLabel className="text-xs text-muted-foreground">Databases</DropdownMenuLabel>
        {(!databases || databases.status === "loading") && list.length === 0 && (
          <p className="flex items-center gap-1.5 px-2 py-1.5 text-xs text-muted-foreground">
            <Loader2 className="size-3 animate-spin" /> Loading…
          </p>
        )}
        {databases?.status === "error" && list.length === 0 && (
          <p className="px-2 py-1.5 text-xs text-destructive">{databases.error.message}</p>
        )}
        {user.map(item)}
        {system.length > 0 && user.length > 0 && <DropdownMenuSeparator />}
        {system.map(item)}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
