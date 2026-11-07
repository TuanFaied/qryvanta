import { create } from "zustand";
import type { Encoding } from "@/services/backend";

/** One editor tab. */
export interface EditorTab {
  id: string;
  /** Shown on the tab, and suggested as the file name by Save As. */
  title: string;
  content: string;
  /** Content at the last open or save; the tab is dirty when content differs. */
  savedContent: string;
  /** File the tab was opened from or saved to, if any. */
  path: string | null;
  /** Encoding of that file; empty for a new document (saved as UTF-8 with BOM). */
  encoding: Encoding | "";
  /** Connection and database the tab runs against. */
  connectionId: string | null;
  database: string | null;
}

export interface NewTabOptions {
  title?: string;
  content?: string;
  path?: string | null;
  encoding?: Encoding | "";
  connectionId?: string | null;
  database?: string | null;
}

interface EditorState {
  tabs: EditorTab[];
  activeId: string | null;
  /** Counter for "Query N" titles; never reused within a run. */
  untitledCount: number;

  newTab: (options?: NewTabOptions) => string;
  /** Opens a file, or activates its tab if it is already open. */
  openDocument: (doc: { path: string; name: string; content: string; encoding: Encoding }, binding: Binding) => string;
  closeTab: (id: string) => void;
  activate: (id: string) => void;
  rename: (id: string, title: string) => void;
  setContent: (id: string, content: string) => void;
  /** Records a successful save: the tab is clean and points at path. */
  markSaved: (id: string, saved: { path: string; name: string; encoding: Encoding; content: string }) => void;
  setBinding: (id: string, binding: Binding) => void;
}

export interface Binding {
  connectionId: string | null;
  database: string | null;
}

let nextId = 1;
const makeId = () => `tab-${Date.now().toString(36)}-${nextId++}`;

export function isDirty(tab: EditorTab): boolean {
  return tab.content !== tab.savedContent;
}

/** Tabs with unsaved changes. A new, empty tab has nothing to lose and is not counted. */
export function unsavedTabs(tabs: EditorTab[]): EditorTab[] {
  return tabs.filter(isDirty);
}

function samePath(a: string | null, b: string): boolean {
  // Windows paths are case-insensitive; this is a convenience check, not a security one.
  return a !== null && a.toLowerCase() === b.toLowerCase();
}

export const useEditorStore = create<EditorState>((set, get) => ({
  tabs: [],
  activeId: null,
  untitledCount: 0,

  newTab: (options = {}) => {
    const id = makeId();
    let { title } = options;
    if (!title) {
      const n = get().untitledCount + 1;
      set({ untitledCount: n });
      title = `Query ${n}`;
    }
    const content = options.content ?? "";
    const tab: EditorTab = {
      id,
      title,
      content,
      savedContent: content,
      path: options.path ?? null,
      encoding: options.encoding ?? "",
      connectionId: options.connectionId ?? null,
      database: options.database ?? null,
    };
    set((s) => ({ tabs: [...s.tabs, tab], activeId: id }));
    return id;
  },

  openDocument: (doc, binding) => {
    const existing = get().tabs.find((t) => samePath(t.path, doc.path));
    if (existing) {
      set({ activeId: existing.id });
      return existing.id;
    }
    // Replace the active tab when it is an untouched, empty "Query N" tab, as editors usually do.
    const active = get().tabs.find((t) => t.id === get().activeId);
    if (active && active.path === null && active.content === "" && active.savedContent === "") {
      set((s) => ({
        tabs: s.tabs.map((t) =>
          t.id === active.id
            ? { ...t, title: doc.name, content: doc.content, savedContent: doc.content, path: doc.path, encoding: doc.encoding }
            : t,
        ),
      }));
      return active.id;
    }
    return get().newTab({
      title: doc.name,
      content: doc.content,
      path: doc.path,
      encoding: doc.encoding,
      ...binding,
    });
  },

  closeTab: (id) =>
    set((s) => {
      const index = s.tabs.findIndex((t) => t.id === id);
      if (index < 0) return s;
      const tabs = s.tabs.filter((t) => t.id !== id);
      let activeId = s.activeId;
      if (activeId === id) {
        // Activate the neighbour on the right, or the left one when closing the last tab.
        activeId = tabs[Math.min(index, tabs.length - 1)]?.id ?? null;
      }
      return { tabs, activeId };
    }),

  activate: (id) => {
    if (get().tabs.some((t) => t.id === id)) set({ activeId: id });
  },

  rename: (id, title) => {
    const trimmed = title.trim();
    if (!trimmed) return;
    set((s) => ({ tabs: s.tabs.map((t) => (t.id === id ? { ...t, title: trimmed } : t)) }));
  },

  setContent: (id, content) =>
    set((s) => ({ tabs: s.tabs.map((t) => (t.id === id && t.content !== content ? { ...t, content } : t)) })),

  markSaved: (id, saved) =>
    set((s) => ({
      tabs: s.tabs.map((t) =>
        t.id === id
          ? {
              ...t,
              // Saving to a new file renames the tab after it.
              title: t.path === saved.path ? t.title : saved.name,
              path: saved.path,
              encoding: saved.encoding,
              savedContent: saved.content,
            }
          : t,
      ),
    })),

  setBinding: (id, binding) =>
    set((s) => ({
      tabs: s.tabs.map((t) =>
        t.id === id && (t.connectionId !== binding.connectionId || t.database !== binding.database) ? { ...t, ...binding } : t,
      ),
    })),
}));

export function useActiveTab(): EditorTab | null {
  return useEditorStore((s) => s.tabs.find((t) => t.id === s.activeId) ?? null);
}
