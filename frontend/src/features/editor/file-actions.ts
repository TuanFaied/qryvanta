import { backend } from "@/services/backend";
import { useEditorStore, type Binding } from "@/stores/editor-store";

export type SaveOutcome = { status: "saved" } | { status: "cancelled" } | { status: "error"; error: string };

/** Shows the open dialog and opens the chosen file in a tab. Returns an error message, if any. */
export async function openFile(binding: Binding): Promise<string | null> {
  try {
    const res = await backend.openFile();
    if (res.cancelled) return null;
    if (res.error || !res.document) return res.error || "The file could not be opened.";
    useEditorStore.getState().openDocument(res.document, binding);
    return null;
  } catch (err) {
    return err instanceof Error ? err.message : String(err);
  }
}

/**
 * Saves a tab: to its file, or through the save dialog when it has none or saveAs is set.
 * The text saved is the text at the moment of the call; edits made while the dialog is open
 * keep the tab dirty.
 */
export async function saveTab(tabId: string, saveAs = false): Promise<SaveOutcome> {
  const tab = useEditorStore.getState().tabs.find((t) => t.id === tabId);
  if (!tab) return { status: "cancelled" };
  const content = tab.content;
  try {
    const res =
      tab.path && !saveAs
        ? await backend.saveFile(tab.path, content, tab.encoding)
        : await backend.saveFileAs(tab.path ? fileName(tab.path) : tab.title, content, tab.encoding);
    if (res.cancelled) return { status: "cancelled" };
    if (res.error) return { status: "error", error: res.error };
    useEditorStore.getState().markSaved(tabId, { path: res.path, name: res.name, encoding: res.encoding, content });
    return { status: "saved" };
  } catch (err) {
    return { status: "error", error: err instanceof Error ? err.message : String(err) };
  }
}

function fileName(path: string): string {
  return path.split(/[\\/]/).pop() ?? path;
}
