import { create } from "zustand";
import { backend, type AppInfo, type Config, type SaveResult, type Theme } from "@/services/backend";

type Status = "loading" | "ready" | "error";

interface AppState {
  status: Status;
  error: string | null;
  info: AppInfo | null;
  config: Config | null;
  load: () => Promise<void>;
  /** Saves settings. Field errors are returned, not thrown. */
  saveConfig: (config: Config) => Promise<SaveResult>;
  setTheme: (theme: Theme) => Promise<void>;
}

export const useAppStore = create<AppState>((set, get) => ({
  status: "loading",
  error: null,
  info: null,
  config: null,

  load: async () => {
    set({ status: "loading", error: null });
    try {
      const [info, config] = await Promise.all([backend.getAppInfo(), backend.getConfig()]);
      set({ status: "ready", info, config });
    } catch (err) {
      set({ status: "error", error: err instanceof Error ? err.message : String(err) });
    }
  },

  saveConfig: async (config) => {
    const res = await backend.saveConfig(config);
    if (res.ok) set({ config: res.config });
    return res;
  },

  setTheme: async (theme) => {
    const current = get().config;
    if (!current) return;
    const next: Config = { ...current, appearance: { ...current.appearance, theme } };
    // Apply immediately so the UI responds instantly; roll back if saving fails.
    set({ config: next });
    try {
      const res = await backend.saveConfig(next);
      if (!res.ok) set({ config: current });
    } catch (err) {
      set({ config: current, error: err instanceof Error ? err.message : String(err) });
    }
  },
}));
