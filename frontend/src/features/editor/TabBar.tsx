import { FileCode2, Loader2, Plus, X } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { IconButton } from "@/components/shell/IconButton";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuShortcut,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";
import { cn } from "@/lib/utils";
import { isDirty, type EditorTab } from "@/stores/editor-store";

interface TabBarProps {
  tabs: EditorTab[];
  activeId: string | null;
  runningTabs?: ReadonlySet<string>;
  onActivate: (id: string) => void;
  onClose: (id: string) => void;
  onRename: (id: string, title: string) => void;
  onNew: () => void;
}

export function TabBar({ tabs, activeId, runningTabs, onActivate, onClose, onRename, onNew }: TabBarProps) {
  const [renaming, setRenaming] = useState<string | null>(null);

  return (
    <div className="flex h-8 shrink-0 items-end border-b bg-panel">
      <div className="flex min-w-0 flex-1 items-end overflow-x-auto" role="tablist" aria-label="Open documents">
        {tabs.map((tab) => (
          <ContextMenu key={tab.id}>
            <ContextMenuTrigger asChild>
              <div
                role="tab"
                aria-selected={tab.id === activeId}
                tabIndex={tab.id === activeId ? 0 : -1}
                title={tab.path ?? tab.title}
                onClick={() => onActivate(tab.id)}
                onDoubleClick={() => setRenaming(tab.id)}
                onAuxClick={(e) => {
                  // Middle click closes, as in browsers and code editors.
                  if (e.button === 1) onClose(tab.id);
                }}
                onKeyDown={(e) => {
                  if (e.key === "F2") setRenaming(tab.id);
                }}
                className={cn(
                  "group flex h-8 max-w-56 shrink-0 cursor-default items-center gap-1.5 border-r border-t-2 pl-3 pr-1 text-xs",
                  tab.id === activeId
                    ? "border-t-primary bg-editor text-foreground"
                    : "border-t-transparent text-muted-foreground hover:bg-accent/40",
                )}
              >
                {runningTabs?.has(tab.id) ? (
                  <Loader2 className="size-3.5 shrink-0 animate-spin text-primary" aria-label="Query running" />
                ) : (
                  <FileCode2 className="size-3.5 shrink-0 text-muted-foreground" />
                )}
                {renaming === tab.id ? (
                  <RenameInput
                    initial={tab.title}
                    onDone={(title) => {
                      if (title !== null) onRename(tab.id, title);
                      setRenaming(null);
                    }}
                  />
                ) : (
                  <span className="truncate">{tab.title}</span>
                )}
                {isDirty(tab) && (
                  <span className="size-1.5 shrink-0 rounded-full bg-foreground/70" role="img" aria-label="Unsaved changes" />
                )}
                <button
                  type="button"
                  aria-label={`Close ${tab.title}`}
                  title="Close (Ctrl+W)"
                  onClick={(e) => {
                    e.stopPropagation();
                    onClose(tab.id);
                  }}
                  className={cn(
                    "flex size-5 shrink-0 items-center justify-center rounded-sm text-muted-foreground hover:bg-accent hover:text-foreground",
                    tab.id === activeId ? "opacity-100" : "opacity-0 group-hover:opacity-100 focus-visible:opacity-100",
                  )}
                >
                  <X className="size-3.5" />
                </button>
              </div>
            </ContextMenuTrigger>
            <ContextMenuContent className="w-48">
              <ContextMenuItem onSelect={() => setRenaming(tab.id)}>
                Rename
                <ContextMenuShortcut>F2</ContextMenuShortcut>
              </ContextMenuItem>
              <ContextMenuSeparator />
              <ContextMenuItem onSelect={() => onClose(tab.id)}>
                Close
                <ContextMenuShortcut>Ctrl+W</ContextMenuShortcut>
              </ContextMenuItem>
            </ContextMenuContent>
          </ContextMenu>
        ))}
      </div>
      <div className="flex h-8 shrink-0 items-center px-1">
        <IconButton icon={Plus} label="New query" hint="Ctrl+N" onClick={onNew} />
      </div>
    </div>
  );
}

function RenameInput({ initial, onDone }: { initial: string; onDone: (title: string | null) => void }) {
  const [value, setValue] = useState(initial);
  const ref = useRef<HTMLInputElement>(null);
  const done = useRef(false);
  const finish = (title: string | null) => {
    if (done.current) return;
    done.current = true;
    onDone(title);
  };

  useEffect(() => {
    ref.current?.focus();
    ref.current?.select();
  }, []);

  return (
    <input
      ref={ref}
      aria-label="Tab name"
      value={value}
      onChange={(e) => setValue(e.target.value)}
      onClick={(e) => e.stopPropagation()}
      onKeyDown={(e) => {
        e.stopPropagation();
        if (e.key === "Enter") finish(value);
        if (e.key === "Escape") finish(null);
      }}
      onBlur={() => finish(value)}
      className="h-5 w-32 rounded-sm border bg-background px-1 text-xs outline-none focus:border-ring"
    />
  );
}
