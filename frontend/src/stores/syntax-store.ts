import { create } from "zustand";
import { backend, type Diagnostic } from "@/services/backend";

/** The last syntax check of a tab. */
export interface SyntaxState {
  /** The exact text that was checked; a result for older text is never shown. */
  text: string;
  diagnostics: Diagnostic[];
  /** False when the parser could not run (then there are no markers, not "no errors"). */
  available: boolean;
  reason: string;
}

interface SyntaxStoreState {
  byTab: Record<string, SyntaxState>;
  /** Checks text for a tab. A newer check for the same tab cancels the older one. */
  check: (tabId: string, text: string) => Promise<void>;
  forget: (tabId: string) => void;
}

const inFlight = new Map<string, AbortController>();

export const useSyntaxStore = create<SyntaxStoreState>((set) => ({
  byTab: {},

  check: async (tabId, text) => {
    inFlight.get(tabId)?.abort();
    const controller = new AbortController();
    inFlight.set(tabId, controller);
    try {
      const res = await backend.checkSyntax(text, controller.signal);
      if (controller.signal.aborted) return;
      set((s) => ({
        byTab: {
          ...s.byTab,
          [tabId]: { text, diagnostics: res.diagnostics ?? [], available: res.available, reason: res.reason },
        },
      }));
    } catch {
      // Cancelled or the backend is unreachable: keep the previous state; the next edit retries.
    } finally {
      if (inFlight.get(tabId) === controller) inFlight.delete(tabId);
    }
  },

  forget: (tabId) =>
    set((s) => {
      inFlight.get(tabId)?.abort();
      inFlight.delete(tabId);
      if (!(tabId in s.byTab)) return s;
      const byTab = { ...s.byTab };
      delete byTab[tabId];
      return { byTab };
    }),
}));

/** Diagnostics of a tab that are current for its text; empty when the check is stale or absent. */
export function currentDiagnostics(state: SyntaxState | undefined, text: string): Diagnostic[] {
  return state && state.available && state.text === text ? state.diagnostics : [];
}

/** Diagnostics on lines startLine..endLine (inclusive), for running only a selection. */
export function diagnosticsInLines(diagnostics: Diagnostic[], startLine: number, endLine: number): Diagnostic[] {
  return diagnostics.filter((d) => d.line >= startLine && d.line <= endLine);
}
