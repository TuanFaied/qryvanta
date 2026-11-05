import {
  AlertTriangle,
  ChevronRight,
  Columns3,
  Copy,
  Database as DatabaseIcon,
  Eye,
  Folder,
  FunctionSquare,
  KeyRound,
  Link2,
  Loader2,
  RefreshCw,
  ScrollText,
  Star,
  Table2,
} from "lucide-react";
import { Fragment, useEffect, useMemo, useState, type KeyboardEvent, type ReactNode } from "react";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";
import { cn } from "@/lib/utils";
import { ObjectKind, type Column, type Database, type SchemaObject } from "@/services/backend";
import { dbKey, objectKey, useSchemaStore, type Load } from "@/stores/schema-store";
import { copyText } from "@/utils/clipboard";
import { qualifiedName, selectTop100 } from "@/utils/sql";

interface SchemaTreeProps {
  connectionId: string;
  /** Lower-case filter text; empty shows everything. */
  filter: string;
  /** Depth of the tree under the connection row. */
  depth?: number;
}

const folders: { kind: ObjectKind; label: string; icon: typeof Table2 }[] = [
  { kind: ObjectKind.KindTable, label: "Tables", icon: Table2 },
  { kind: ObjectKind.KindView, label: "Views", icon: Eye },
  { kind: ObjectKind.KindProcedure, label: "Stored procedures", icon: ScrollText },
  { kind: ObjectKind.KindFunction, label: "Functions", icon: FunctionSquare },
];

/** Databases, object folders, objects and columns of one connected server. */
export function SchemaTree({ connectionId, filter, depth = 1 }: SchemaTreeProps) {
  const databases = useSchemaStore((s) => s.databases[connectionId]);
  const loadDatabases = useSchemaStore((s) => s.loadDatabases);
  const expanded = useSchemaStore((s) => s.expanded);
  const setExpanded = useSchemaStore((s) => s.setExpanded);
  const [notice, setNotice] = useState<string | null>(null);

  // Load on first show, and again after a refresh drops the cached list.
  const missing = databases === undefined;
  useEffect(() => {
    if (missing) void loadDatabases(connectionId);
  }, [connectionId, missing, loadDatabases]);

  useEffect(() => {
    if (!notice) return;
    const t = setTimeout(() => setNotice(null), 2000);
    return () => clearTimeout(t);
  }, [notice]);

  const copy = async (text: string, what: string) => {
    setNotice((await copyText(text)) ? `Copied ${what}` : "Could not access the clipboard");
  };

  if (!databases || (databases.status === "loading" && !databases.data)) {
    return <StatusRow depth={depth} loading text="Loading databases…" />;
  }
  if (databases.status === "error" && !databases.data) {
    return <StatusRow depth={depth} error text={databases.error.message} onRetry={() => void loadDatabases(connectionId, true)} />;
  }

  const all = databases.data ?? [];
  const user = all.filter((d) => !d.system);
  const system = all.filter((d) => d.system);
  const systemKey = `${connectionId}\u0000#system`;

  return (
    <>
      {user.map((d) => (
        <DatabaseNode key={d.name} connectionId={connectionId} database={d} depth={depth} filter={filter} onCopy={copy} />
      ))}
      {system.length > 0 && (
        <>
          <TreeRow
            depth={depth}
            icon={<Folder className="size-3.5 text-muted-foreground" />}
            label="System databases"
            detail={String(system.length)}
            expandable
            expanded={!!expanded[systemKey]}
            onToggle={() => setExpanded(systemKey, !expanded[systemKey])}
          />
          {expanded[systemKey] &&
            system.map((d) => (
              <DatabaseNode key={d.name} connectionId={connectionId} database={d} depth={depth + 1} filter={filter} onCopy={copy} />
            ))}
        </>
      )}
      <li aria-live="polite" className="sr-only">
        {notice}
      </li>
      {notice && (
        <li role="status" className="mx-2 my-1 rounded-sm bg-accent px-2 py-1 text-[11px] text-muted-foreground">
          {notice}
        </li>
      )}
    </>
  );
}

function DatabaseNode({
  connectionId,
  database,
  depth,
  filter,
  onCopy,
}: {
  connectionId: string;
  database: Database;
  depth: number;
  filter: string;
  onCopy: (text: string, what: string) => void;
}) {
  const key = dbKey(connectionId, database.name);
  const open = useSchemaStore((s) => !!s.expanded[key]);
  const objects = useSchemaStore((s) => s.objects[key]);
  const current = useSchemaStore((s) => s.currentDatabase[connectionId]);
  const { setExpanded, loadObjects, setCurrentDatabase } = useSchemaStore.getState();

  const toggle = () => {
    if (!database.accessible) return;
    if (!open) void loadObjects(connectionId, database.name);
    setExpanded(key, !open);
  };
  const isCurrent = current === database.name;

  const title = !database.accessible
    ? `${database.name}: not accessible (${database.state.toLowerCase()}). The login has no access to this database.`
    : `${database.name}${database.readOnly ? " (read-only)" : ""}. Double-click to make it the current database.`;

  return (
    <>
      <TreeRow
        depth={depth}
        icon={<DatabaseIcon className={cn("size-3.5", isCurrent ? "text-primary" : "text-muted-foreground")} />}
        label={database.name}
        detail={!database.accessible ? database.state === "ONLINE" ? "no access" : database.state.toLowerCase() : database.readOnly ? "read-only" : undefined}
        title={title}
        dimmed={!database.accessible}
        badge={isCurrent ? <Star className="size-3 fill-primary text-primary" aria-label="current database" /> : undefined}
        expandable={database.accessible}
        expanded={open}
        loading={objects?.status === "loading"}
        onToggle={toggle}
        onActivate={() => database.accessible && setCurrentDatabase(connectionId, database.name)}
        menu={
          <>
            <ContextMenuItem className="text-xs" disabled={!database.accessible || isCurrent} onSelect={() => setCurrentDatabase(connectionId, database.name)}>
              <Star className="size-3.5" /> Use as current database
            </ContextMenuItem>
            <ContextMenuItem className="text-xs" disabled={!database.accessible} onSelect={() => void loadObjects(connectionId, database.name, true)}>
              <RefreshCw className="size-3.5" /> Refresh
            </ContextMenuItem>
            <ContextMenuSeparator />
            <ContextMenuItem className="text-xs" onSelect={() => onCopy(database.name, "database name")}>
              <Copy className="size-3.5" /> Copy name
            </ContextMenuItem>
          </>
        }
      />
      {open && (
        <ObjectFolders connectionId={connectionId} database={database.name} objects={objects} depth={depth + 1} filter={filter} onCopy={onCopy} />
      )}
    </>
  );
}

function ObjectFolders({
  connectionId,
  database,
  objects,
  depth,
  filter,
  onCopy,
}: {
  connectionId: string;
  database: string;
  objects: Load<SchemaObject[]> | undefined;
  depth: number;
  filter: string;
  onCopy: (text: string, what: string) => void;
}) {
  const expanded = useSchemaStore((s) => s.expanded);
  const { setExpanded, loadObjects } = useSchemaStore.getState();

  const filtered = useMemo(() => {
    const list = objects?.data ?? [];
    if (!filter) return list;
    return list.filter((o) => `${o.schema}.${o.name}`.toLowerCase().includes(filter));
  }, [objects?.data, filter]);

  if (!objects || (objects.status === "loading" && !objects.data)) return <StatusRow depth={depth} loading text="Loading objects…" />;
  if (objects.status === "error" && !objects.data) {
    return <StatusRow depth={depth} error text={objects.error.message} onRetry={() => void loadObjects(connectionId, database, true)} />;
  }

  return (
    <>
      {folders.map((f) => {
        const items = filtered.filter((o) => o.kind === f.kind);
        const key = `${dbKey(connectionId, database)}\u0000#${f.kind}`;
        // While filtering, folders with matches open automatically and empty ones are hidden.
        if (filter && items.length === 0) return null;
        const open = filter ? true : !!expanded[key];
        return (
          <Fragment key={f.kind}>
            <TreeRow
              depth={depth}
              icon={<f.icon className="size-3.5 text-muted-foreground" />}
              label={f.label}
              detail={String(items.length)}
              expandable={items.length > 0}
              expanded={open}
              onToggle={() => setExpanded(key, !open)}
            />
            {open &&
              items.map((o) => (
                <ObjectNode key={`${o.schema}.${o.name}`} connectionId={connectionId} database={database} object={o} depth={depth + 1} onCopy={onCopy} />
              ))}
          </Fragment>
        );
      })}
    </>
  );
}

function ObjectNode({
  connectionId,
  database,
  object: o,
  depth,
  onCopy,
}: {
  connectionId: string;
  database: string;
  object: SchemaObject;
  depth: number;
  onCopy: (text: string, what: string) => void;
}) {
  const key = objectKey(connectionId, database, o.schema, o.name);
  const open = useSchemaStore((s) => !!s.expanded[key]);
  const columns = useSchemaStore((s) => s.columns[key]);
  const { setExpanded, loadColumns } = useSchemaStore.getState();
  const hasColumns = o.kind === ObjectKind.KindTable || o.kind === ObjectKind.KindView;
  const Icon = folders.find((f) => f.kind === o.kind)?.icon ?? Table2;

  const toggle = () => {
    if (!hasColumns) return;
    if (!open) void loadColumns(connectionId, database, o.schema, o.name);
    setExpanded(key, !open);
  };

  const copySelect = async () => {
    // Use explicit column names when they are loaded (or can be loaded); fall back to *.
    await loadColumns(connectionId, database, o.schema, o.name);
    const cols = useSchemaStore.getState().columns[key];
    const names = cols?.status === "ready" ? cols.data.map((c) => c.name) : [];
    onCopy(selectTop100(o.schema, o.name, names), "SELECT statement");
  };

  return (
    <>
      <TreeRow
        depth={depth}
        icon={<Icon className="size-3.5 text-muted-foreground" />}
        label={`${o.schema}.${o.name}`}
        title={`${qualifiedName(o.schema, o.name)} in ${database}`}
        expandable={hasColumns}
        expanded={open}
        loading={columns?.status === "loading"}
        onToggle={toggle}
        menu={
          <>
            <ContextMenuItem className="text-xs" onSelect={() => onCopy(qualifiedName(o.schema, o.name), "name")}>
              <Copy className="size-3.5" /> Copy name
            </ContextMenuItem>
            {hasColumns && (
              <ContextMenuItem className="text-xs" onSelect={() => void copySelect()}>
                <Copy className="size-3.5" /> Copy SELECT TOP (100)
              </ContextMenuItem>
            )}
            {hasColumns && (
              <ContextMenuItem className="text-xs" onSelect={() => void loadColumns(connectionId, database, o.schema, o.name, true)}>
                <RefreshCw className="size-3.5" /> Refresh columns
              </ContextMenuItem>
            )}
          </>
        }
      />
      {open && hasColumns && <ColumnList columns={columns} depth={depth + 1} onRetry={() => void loadColumns(connectionId, database, o.schema, o.name, true)} onCopy={onCopy} />}
    </>
  );
}

function ColumnList({
  columns,
  depth,
  onRetry,
  onCopy,
}: {
  columns: Load<Column[]> | undefined;
  depth: number;
  onRetry: () => void;
  onCopy: (text: string, what: string) => void;
}) {
  if (!columns || (columns.status === "loading" && !columns.data)) return <StatusRow depth={depth} loading text="Loading columns…" />;
  if (columns.status === "error" && !columns.data) return <StatusRow depth={depth} error text={columns.error.message} onRetry={onRetry} />;
  return (
    <>
      {(columns.data ?? []).map((c) => {
        const flags = [c.identity && "identity", c.computed && "computed", c.nullable ? "NULL" : "NOT NULL"].filter(Boolean).join(", ");
        const title = [
          `${c.name} ${c.dataType} ${c.nullable ? "NULL" : "NOT NULL"}`,
          c.primaryKey && "Primary key",
          c.references && `References ${c.references}`,
          c.identity && "Identity",
          c.computed && "Computed",
        ]
          .filter(Boolean)
          .join("\n");
        return (
          <TreeRow
            key={c.ordinal}
            depth={depth}
            icon={
              c.primaryKey ? (
                <KeyRound className="size-3.5 text-warning" aria-label="primary key" />
              ) : c.references ? (
                <Link2 className="size-3.5 text-info" aria-label="foreign key" />
              ) : (
                <Columns3 className="size-3.5 text-muted-foreground/70" />
              )
            }
            label={c.name}
            detail={`${c.dataType}, ${flags}`}
            title={title}
            menu={
              <ContextMenuItem className="text-xs" onSelect={() => onCopy(`[${c.name.replaceAll("]", "]]")}]`, "column name")}>
                <Copy className="size-3.5" /> Copy name
              </ContextMenuItem>
            }
          />
        );
      })}
    </>
  );
}

function StatusRow({
  depth,
  text,
  loading,
  error,
  onRetry,
}: {
  depth: number;
  text: string;
  loading?: boolean;
  error?: boolean;
  onRetry?: () => void;
}) {
  return (
    <li role="treeitem" aria-selected={false} className="flex items-center gap-1.5 py-1 pr-2 text-[11px] text-muted-foreground" style={{ paddingLeft: indent(depth) + 18 }}>
      {loading && <Loader2 className="size-3 animate-spin" />}
      {error && <AlertTriangle className="size-3 shrink-0 text-destructive" />}
      <span className={cn("min-w-0 flex-1", error ? "break-words text-destructive" : "truncate")}>{text}</span>
      {onRetry && (
        <button type="button" onClick={onRetry} className="shrink-0 rounded-sm px-1 underline-offset-2 hover:underline">
          Retry
        </button>
      )}
    </li>
  );
}

const indent = (depth: number) => 8 + depth * 12;

interface TreeRowProps {
  depth: number;
  icon: ReactNode;
  label: string;
  detail?: string | undefined;
  title?: string;
  badge?: ReactNode;
  dimmed?: boolean;
  expandable?: boolean;
  expanded?: boolean;
  loading?: boolean;
  onToggle?: () => void;
  /** Double-click or Enter on a non-expandable row. */
  onActivate?: () => void;
  menu?: ReactNode;
}

/** One row of the explorer tree, with keyboard support and an optional context menu. */
export function TreeRow({ depth, icon, label, detail, title, badge, dimmed, expandable, expanded, loading, onToggle, onActivate, menu }: TreeRowProps) {
  const onKeyDown = (e: KeyboardEvent<HTMLLIElement>) => {
    if (e.key === "ArrowRight" && expandable && !expanded) onToggle?.();
    else if (e.key === "ArrowLeft" && expandable && expanded) onToggle?.();
    else if (e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      if (expandable) onToggle?.();
      else onActivate?.();
    } else return;
    e.stopPropagation();
  };

  const row = (
    <li
      role="treeitem"
      aria-expanded={expandable ? !!expanded : undefined}
      aria-selected={false}
      aria-label={label}
      tabIndex={0}
      title={title}
      onClick={expandable ? onToggle : undefined}
      onDoubleClick={onActivate}
      onKeyDown={onKeyDown}
      className={cn(
        "mx-1 flex cursor-default items-center gap-1.5 rounded-sm py-0.5 pr-2 outline-none select-none hover:bg-accent/60 focus-visible:ring-1 focus-visible:ring-ring",
        dimmed && "opacity-50",
      )}
      style={{ paddingLeft: indent(depth) }}
    >
      <ChevronRight
        aria-hidden="true"
        className={cn("size-3 shrink-0 text-muted-foreground transition-transform", expanded && "rotate-90", !expandable && "invisible")}
      />
      <span className="shrink-0">{icon}</span>
      {/* The name keeps priority over the detail text: detail truncates first. */}
      <span className="max-w-[75%] min-w-0 shrink-0 truncate text-xs">{label}</span>
      {badge}
      {detail && <span className="min-w-0 shrink truncate font-mono text-[10px] text-muted-foreground">{detail}</span>}
      {loading && <Loader2 className="ml-auto size-3 shrink-0 animate-spin text-muted-foreground" aria-label="Loading" />}
    </li>
  );

  if (!menu) return row;
  return (
    <ContextMenu>
      <ContextMenuTrigger asChild>{row}</ContextMenuTrigger>
      <ContextMenuContent className="w-52">{menu}</ContextMenuContent>
    </ContextMenu>
  );
}
