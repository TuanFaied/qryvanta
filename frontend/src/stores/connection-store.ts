import { create } from "zustand";
import {
  backend,
  type ConnectionDTO,
  type ConnectionError,
  type ConnectionInput,
  type SaveConnectionResult,
  type ServerInfo,
} from "@/services/backend";

type Status = "idle" | "loading" | "ready" | "error";

interface ConnectionState {
  status: Status;
  loadError: string | null;
  connections: ConnectionDTO[];
  /** The connection the workspace runs queries against. */
  activeId: string | null;
  activeServer: ServerInfo | null;
  /** Connection currently being opened, for spinners. */
  connectingId: string | null;
  /** Last connect failure, shown next to the selector until the next attempt. */
  connectError: { id: string; error: ConnectionError } | null;

  load: () => Promise<void>;
  save: (input: ConnectionInput) => Promise<SaveConnectionResult>;
  remove: (id: string) => Promise<void>;
  connect: (id: string) => Promise<boolean>;
  disconnect: () => Promise<void>;
  clearConnectError: () => void;
}

export const useConnectionStore = create<ConnectionState>((set, get) => ({
  status: "idle",
  loadError: null,
  connections: [],
  activeId: null,
  activeServer: null,
  connectingId: null,
  connectError: null,

  load: async () => {
    set({ status: "loading", loadError: null });
    try {
      const connections = await backend.listConnections();
      const { activeId } = get();
      // Drop the active connection if it was deleted or closed elsewhere.
      const stillActive = connections.some((c) => c.profile.id === activeId && c.connected);
      set({
        status: "ready",
        connections,
        ...(stillActive ? {} : { activeId: null, activeServer: null }),
      });
    } catch (err) {
      set({ status: "error", loadError: err instanceof Error ? err.message : String(err) });
    }
  },

  save: async (input) => {
    const res = await backend.saveConnection(input);
    if (res.ok) {
      // Saving an existing connection closes its open handle in the backend.
      if (input.profile.id && input.profile.id === get().activeId) {
        set({ activeId: null, activeServer: null });
      }
      await get().load();
    }
    return res;
  },

  remove: async (id) => {
    await backend.deleteConnection(id);
    if (get().activeId === id) set({ activeId: null, activeServer: null });
    await get().load();
  },

  connect: async (id) => {
    const previous = get().activeId;
    set({ connectingId: id, connectError: null });
    try {
      const res = await backend.connect(id);
      if (!res.ok) {
        if (res.error) set({ connectError: { id, error: res.error } });
        return false;
      }
      // One active connection at a time: close the previous one.
      if (previous && previous !== id) await backend.disconnect(previous).catch(() => {});
      set({ activeId: id, activeServer: res.server });
      return true;
    } finally {
      set({ connectingId: null });
      await get().load();
    }
  },

  disconnect: async () => {
    const id = get().activeId;
    if (!id) return;
    await backend.disconnect(id);
    set({ activeId: null, activeServer: null });
    await get().load();
  },

  clearConnectError: () => set({ connectError: null }),
}));

/** The active connection's DTO, if any. */
export function useActiveConnection(): ConnectionDTO | null {
  return useConnectionStore((s) => s.connections.find((c) => c.profile.id === s.activeId) ?? null);
}
