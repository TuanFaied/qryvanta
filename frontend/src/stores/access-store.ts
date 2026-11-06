import { create } from "zustand";
import { AccessLevel, backend, type WriteAccess } from "@/services/backend";

export const accessKey = (conn: string, db: string) => `${conn}\u0000${db}`;

interface AccessState {
  /** Write access per connection and database; missing while the check runs. */
  results: Record<string, WriteAccess>;
  loading: Record<string, boolean>;
  check: (conn: string, db: string, force?: boolean) => Promise<void>;
  forget: (conn: string) => void;
}

export const useAccessStore = create<AccessState>((set, get) => ({
  results: {},
  loading: {},

  check: async (conn, db, force = false) => {
    const key = accessKey(conn, db);
    if (!force && (get().results[key] || get().loading[key])) return;
    set((s) => ({ loading: { ...s.loading, [key]: true } }));
    let result: WriteAccess;
    try {
      result = await backend.checkWriteAccess(conn, db);
    } catch (err) {
      result = { database: db, level: AccessLevel.AccessUnknown, reasons: [], error: err instanceof Error ? err.message : String(err) };
    }
    set((s) => ({ results: { ...s.results, [key]: result }, loading: { ...s.loading, [key]: false } }));
  },

  forget: (conn) =>
    set((s) => {
      const keep = <T,>(m: Record<string, T>) => Object.fromEntries(Object.entries(m).filter(([k]) => !k.startsWith(conn + "\u0000")));
      return { results: keep(s.results), loading: keep(s.loading) };
    }),
}));
