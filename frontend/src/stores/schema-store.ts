import { create } from "zustand";
import { backend, type Column, type Database, type MetadataError, type SchemaObject } from "@/services/backend";

/** One lazily loaded tree node. */
export type Load<T> =
  | { status: "loading"; data: T | null; error: null }
  | { status: "ready"; data: T; error: null }
  | { status: "error"; data: T | null; error: MetadataError };

export const dbKey = (conn: string, db: string) => `${conn}\u0000${db}`;
export const objectKey = (conn: string, db: string, schema: string, name: string) =>
  `${conn}\u0000${db}\u0000${schema}\u0000${name}`;

interface SchemaState {
  databases: Record<string, Load<Database[]>>;
  objects: Record<string, Load<SchemaObject[]>>;
  columns: Record<string, Load<Column[]>>;
  /** Expanded tree nodes, by node key. */
  expanded: Record<string, boolean>;
  /** Database the workspace uses, per connection. */
  currentDatabase: Record<string, string>;

  loadDatabases: (conn: string, force?: boolean) => Promise<void>;
  loadObjects: (conn: string, db: string, force?: boolean) => Promise<void>;
  loadColumns: (conn: string, db: string, schema: string, name: string, force?: boolean) => Promise<void>;
  setExpanded: (key: string, open: boolean) => void;
  setCurrentDatabase: (conn: string, db: string) => void;
  /** Drops cached metadata for a connection (refresh, disconnect, delete). */
  forget: (conn: string) => void;
}

const failure = (err: unknown): MetadataError => ({
  code: "server",
  serverErrorNumber: 0,
  message: err instanceof Error ? err.message : String(err),
});

// Generic loader: skips a request when the node is loaded or loading, unless forced.
async function load<T>(
  get: () => Record<string, Load<T>>,
  set: (key: string, value: Load<T>) => void,
  key: string,
  force: boolean,
  fetch: () => Promise<{ data: T; error: MetadataError | null }>,
): Promise<void> {
  const existing = get()[key];
  if (!force && existing && existing.status !== "error") return;
  set(key, { status: "loading", data: existing?.data ?? null, error: null });
  try {
    const res = await fetch();
    set(key, res.error ? { status: "error", data: existing?.data ?? null, error: res.error } : { status: "ready", data: res.data, error: null });
  } catch (err) {
    set(key, { status: "error", data: existing?.data ?? null, error: failure(err) });
  }
}

export const useSchemaStore = create<SchemaState>((set, get) => ({
  databases: {},
  objects: {},
  columns: {},
  expanded: {},
  currentDatabase: {},

  loadDatabases: (conn, force = false) =>
    load(
      () => get().databases,
      (k, v) => set((s) => ({ databases: { ...s.databases, [k]: v } })),
      conn,
      force,
      () => backend.listDatabases(conn),
    ),

  loadObjects: (conn, db, force = false) =>
    load(
      () => get().objects,
      (k, v) => set((s) => ({ objects: { ...s.objects, [k]: v } })),
      dbKey(conn, db),
      force,
      () => backend.listObjects(conn, db),
    ),

  loadColumns: (conn, db, schema, name, force = false) =>
    load(
      () => get().columns,
      (k, v) => set((s) => ({ columns: { ...s.columns, [k]: v } })),
      objectKey(conn, db, schema, name),
      force,
      () => backend.listColumns(conn, db, schema, name),
    ),

  setExpanded: (key, open) => set((s) => ({ expanded: { ...s.expanded, [key]: open } })),

  setCurrentDatabase: (conn, db) => set((s) => ({ currentDatabase: { ...s.currentDatabase, [conn]: db } })),

  forget: (conn) =>
    set((s) => {
      const keep = <T,>(m: Record<string, T>) =>
        Object.fromEntries(Object.entries(m).filter(([k]) => k !== conn && !k.startsWith(conn + "\u0000")));
      return { databases: keep(s.databases), objects: keep(s.objects), columns: keep(s.columns), expanded: keep(s.expanded) };
    }),
}));
