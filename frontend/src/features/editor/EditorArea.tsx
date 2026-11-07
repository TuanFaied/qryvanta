import { AlertTriangle, Database, FolderOpen, Loader2, Save, Wand2, X } from "lucide-react";
import { lazy, Suspense, useCallback, useEffect, useRef, useState, type ReactNode, type RefObject } from "react";
import { IconButton } from "@/components/shell/IconButton";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { useResolvedTheme } from "@/features/theme/theme";
import { backend, type Config } from "@/services/backend";
import { isDirty, unsavedTabs, useEditorStore, type Binding, type EditorTab } from "@/stores/editor-store";
import type { EditorCommand, SqlEditorHandle } from "./editor-types";
import { openFile, saveTab } from "./file-actions";
import { TabBar } from "./TabBar";

// Monaco is a large chunk; it loads with the first editor tab, not with the app shell.
const SqlEditor = lazy(() => import("./SqlEditor"));

/** Actions the welcome page offers while no tab is open. */
export interface EditorActions {
  newQuery: () => void;
  openFile: () => void;
}

interface EditorAreaProps {
  config: Config;
  /** Connection and database new tabs are bound to. */
  binding: Binding;
  connectionName: (id: string) => string | null;
  onCommand: (command: EditorCommand, tab: EditorTab) => void;
  /** True while the active tab's query runs (enables Esc as cancel). */
  executing?: boolean;
  /** Tabs with a query in progress, marked on the tab bar. */
  runningTabs?: ReadonlySet<string>;
  /** Lets the workbench read the selection and move the cursor. */
  editorRef?: RefObject<SqlEditorHandle | null>;
  renderWelcome: (actions: EditorActions) => ReactNode;
}

export function EditorArea({
  config,
  binding,
  connectionName,
  onCommand,
  executing,
  runningTabs,
  editorRef,
  renderWelcome,
}: EditorAreaProps) {
  const { tabs, activeId } = useEditorStore();
  const store = useEditorStore.getState;
  const active = tabs.find((t) => t.id === activeId) ?? null;
  const theme = useResolvedTheme(config.appearance.theme);
  const ownRef = useRef<SqlEditorHandle>(null);
  const editor = editorRef ?? ownRef;
  const [notice, setNotice] = useState<string | null>(null);
  const [closing, setClosing] = useState<string | null>(null);
  const [closeWindow, setCloseWindow] = useState(false);

  const bindingRef = useRef(binding);
  bindingRef.current = binding;

  const newQuery = useCallback(() => {
    store().newTab({ ...bindingRef.current });
  }, [store]);

  const open = useCallback(async () => {
    setNotice(await openFile(bindingRef.current));
  }, []);

  const save = useCallback(async (id: string, saveAs = false) => {
    const res = await saveTab(id, saveAs);
    if (res.status === "error") setNotice(res.error);
    else if (res.status === "saved") setNotice(null);
    return res.status === "saved";
  }, []);

  const requestClose = useCallback(
    (id: string) => {
      const tab = store().tabs.find((t) => t.id === id);
      if (!tab) return;
      if (isDirty(tab)) setClosing(id);
      else store().closeTab(id);
    },
    [store],
  );

  // File and tab shortcuts work everywhere in the workbench, including inside Monaco.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (!(e.ctrlKey || e.metaKey) || e.altKey) return;
      // Leave dialogs (settings, connections) alone.
      if (e.target instanceof Element && e.target.closest("[role=dialog],[role=alertdialog]")) return;
      const key = e.key.toLowerCase();
      const id = store().activeId;
      let handled = true;
      if (key === "n" && !e.shiftKey) newQuery();
      else if (key === "o" && !e.shiftKey) void open();
      else if (key === "s" && id) void save(id, e.shiftKey);
      else if (key === "w" && !e.shiftKey && id) requestClose(id);
      else handled = false;
      if (handled) {
        e.preventDefault();
        e.stopPropagation();
      }
    };
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  }, [newQuery, open, save, requestClose, store]);

  // Tell the backend whether closing the window would lose work, and ask when it would.
  const hasUnsaved = tabs.some(isDirty);
  const reported = useRef(false);
  useEffect(() => {
    if (reported.current === hasUnsaved) return;
    reported.current = hasUnsaved;
    backend.setUnsavedChanges(hasUnsaved).catch(() => {});
  }, [hasUnsaved]);
  useEffect(() => backend.onCloseRequested(() => setCloseWindow(true)), []);

  const closingTab = tabs.find((t) => t.id === closing) ?? null;
  const unsaved = unsavedTabs(tabs);
  const boundTo = active?.connectionId ? connectionName(active.connectionId) : null;

  return (
    <main className="flex h-full flex-col" aria-label="Workspace">
      {tabs.length > 0 && (
        <TabBar
          tabs={tabs}
          activeId={activeId}
          runningTabs={runningTabs}
          onActivate={(id) => store().activate(id)}
          onClose={requestClose}
          onRename={(id, title) => store().rename(id, title)}
          onNew={newQuery}
        />
      )}

      {active && (
        <div className="flex h-8 shrink-0 items-center gap-0.5 border-b bg-editor px-1.5" role="toolbar" aria-label="Editor">
          <IconButton icon={FolderOpen} label="Open file" hint="Ctrl+O" onClick={() => void open()} />
          <IconButton icon={Save} label="Save" hint="Ctrl+S · Save As: Ctrl+Shift+S" onClick={() => void save(active.id)} />
          <IconButton icon={Wand2} label="Format document" hint="Shift+Alt+F" onClick={() => editor.current?.format()} />
          <div className="ml-auto flex min-w-0 items-center gap-1.5 pr-1 text-[11px] text-muted-foreground">
            <Database className="size-3.5 shrink-0" />
            <span className="truncate" aria-label="Tab connection">
              {boundTo ? `${boundTo}${active.database ? ` · ${active.database}` : ""}` : "Not connected"}
            </span>
          </div>
        </div>
      )}

      {notice && (
        <div role="alert" className="flex shrink-0 items-start gap-2 border-b border-warning/40 bg-warning/10 px-3 py-1.5 text-xs">
          <AlertTriangle className="mt-0.5 size-3.5 shrink-0 text-warning" />
          <span className="selectable min-w-0 flex-1">{notice}</span>
          <button type="button" aria-label="Dismiss" onClick={() => setNotice(null)} className="text-muted-foreground hover:text-foreground">
            <X className="size-3.5" />
          </button>
        </div>
      )}

      <div className="min-h-0 flex-1">
        {active ? (
          <Suspense
            fallback={
              <div className="flex h-full items-center justify-center bg-editor" aria-busy="true">
                <Loader2 className="size-5 animate-spin text-muted-foreground" aria-label="Loading editor" />
              </div>
            }
          >
            <SqlEditor
              handleRef={editor}
              tabId={active.id}
              tabIds={tabs.map((t) => t.id)}
              value={active.content}
              theme={theme}
              fontSize={config.appearance.editorFontSize}
              formatting={config.formatting}
              executing={executing}
              onChange={(id, text) => store().setContent(id, text)}
              onCommand={(command) => onCommand(command, active)}
              onFormatResult={setNotice}
            />
          </Suspense>
        ) : (
          renderWelcome({ newQuery, openFile: () => void open() })
        )}
      </div>

      <AlertDialog open={closingTab !== null} onOpenChange={(o) => !o && setClosing(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Save changes to {closingTab?.title}?</AlertDialogTitle>
            <AlertDialogDescription>Your changes will be lost if you close the tab without saving.</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <Button
              variant="outline"
              onClick={() => {
                if (closingTab) store().closeTab(closingTab.id);
                setClosing(null);
              }}
            >
              Don't save
            </Button>
            <AlertDialogAction
              onClick={async (e) => {
                e.preventDefault();
                if (!closingTab) return;
                const id = closingTab.id;
                setClosing(null);
                if (await save(id)) store().closeTab(id);
              }}
            >
              Save
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog open={closeWindow} onOpenChange={setCloseWindow}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Close Qryvanta?</AlertDialogTitle>
            <AlertDialogDescription>
              {unsaved.length === 1
                ? `${unsaved[0]?.title} has unsaved changes.`
                : `${unsaved.length} tabs have unsaved changes: ${unsaved.map((t) => t.title).join(", ")}.`}{" "}
              They will be lost.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Keep working</AlertDialogCancel>
            <AlertDialogAction
              className="bg-destructive text-white hover:bg-destructive/90"
              onClick={() => void backend.closeWindow().catch(() => {})}
            >
              Close without saving
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </main>
  );
}
