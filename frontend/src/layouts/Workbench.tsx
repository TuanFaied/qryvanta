import { useCallback, useEffect, useRef, useState } from "react";
import { useDefaultLayout, usePanelRef, type PanelImperativeHandle, type PanelSize } from "react-resizable-panels";
import { Header } from "@/components/shell/Header";
import { StatusBar } from "@/components/shell/StatusBar";
import { ResizableHandle, ResizablePanel, ResizablePanelGroup } from "@/components/ui/resizable";
import { AnalysisPanel } from "@/features/analysis/AnalysisPanel";
import { ConnectionDialog } from "@/features/connections/ConnectionDialog";
import { useConnectionDialog } from "@/features/connections/connection-dialog-store";
import { ConnectionSelector } from "@/features/connections/ConnectionSelector";
import { DeleteConnectionDialog } from "@/features/connections/DeleteConnectionDialog";
import { EditorArea } from "@/features/editor/EditorArea";
import { DatabaseSelector } from "@/features/explorer/DatabaseSelector";
import { ExplorerPanel } from "@/features/explorer/ExplorerPanel";
import { SchemaTree } from "@/features/explorer/SchemaTree";
import { SettingsDialog } from "@/features/settings/SettingsDialog";
import { BottomPanel } from "@/features/workspace/BottomPanel";
import { WelcomePage } from "@/features/workspace/WelcomePage";
import { backend, type AppInfo, type Config, type SaveResult, type Theme } from "@/services/backend";
import { useConnectionStore } from "@/stores/connection-store";
import { useEditorStore } from "@/stores/editor-store";
import { useSchemaStore } from "@/stores/schema-store";
import { accessKey, useAccessStore } from "@/stores/access-store";
import { safeStorage } from "@/utils/safe-storage";
import type { PanelId } from "./panels";

interface WorkbenchProps {
  info: AppInfo;
  config: Config;
  onThemeChange: (theme: Theme) => void;
  onSaveConfig: (config: Config) => Promise<SaveResult>;
  onLoadDefaults: () => Promise<Config>;
  onOpenLogFolder: () => Promise<void>;
}

export function Workbench({ info, config, onThemeChange, onSaveConfig, onLoadDefaults, onOpenLogFolder }: WorkbenchProps) {
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [visible, setVisible] = useState<Record<PanelId, boolean>>({ explorer: true, analysis: true, bottom: true });

  const refs: Record<PanelId, React.RefObject<PanelImperativeHandle | null>> = {
    explorer: usePanelRef(),
    analysis: usePanelRef(),
    bottom: usePanelRef(),
  };

  // Panel sizes (including collapsed state) are a per-user convenience kept in localStorage.
  const mainLayout = useDefaultLayout({ id: "qryvanta.layout.main", storage: safeStorage });
  const centerLayout = useDefaultLayout({ id: "qryvanta.layout.center", storage: safeStorage });

  const trackVisibility = useCallback(
    (id: PanelId) => (size: PanelSize) => {
      const isVisible = size.inPixels > 0;
      setVisible((v) => (v[id] === isVisible ? v : { ...v, [id]: isVisible }));
    },
    [],
  );

  const togglePanel = (id: PanelId) => {
    const panel = refs[id].current;
    if (!panel) return;
    if (panel.isCollapsed()) panel.expand();
    else panel.collapse();
  };

  const openSettings = () => setSettingsOpen(true);

  const conn = useConnectionStore();
  const dialog = useConnectionDialog();
  const active = conn.connections.find((c) => c.profile.id === conn.activeId) ?? null;
  const connect = (id: string) => void conn.connect(id);

  const schema = useSchemaStore();
  const activeId = conn.activeId;
  const currentDatabase = activeId ? (schema.currentDatabase[activeId] ?? null) : null;
  const { setCurrentDatabase, forget } = useSchemaStore.getState();

  // A new connection starts in the login's default database.
  const serverDatabase = conn.activeServer?.database;
  useEffect(() => {
    if (activeId && serverDatabase && !useSchemaStore.getState().currentDatabase[activeId]) {
      setCurrentDatabase(activeId, serverDatabase);
    }
  }, [activeId, serverDatabase, setCurrentDatabase]);

  // Cached metadata and permissions belong to an open connection: drop them when it closes.
  const forgetAccess = useAccessStore((s) => s.forget);
  const previousActive = useRef<string | null>(null);
  useEffect(() => {
    const prev = previousActive.current;
    if (prev && prev !== activeId) {
      forget(prev);
      forgetAccess(prev);
    }
    previousActive.current = activeId;
  }, [activeId, forget, forgetAccess]);

  // Check what the login could change whenever the connection or current database changes.
  const checkAccess = useAccessStore((s) => s.check);
  const access = useAccessStore((s) => (activeId && currentDatabase ? s.results[accessKey(activeId, currentDatabase)] : undefined));
  useEffect(() => {
    if (activeId && currentDatabase) void checkAccess(activeId, currentDatabase);
  }, [activeId, currentDatabase, checkAccess]);

  // Each editor tab remembers its connection and database. Changing them in the header
  // updates the active tab; switching tabs restores the tab's connection and database.
  const activeTabId = useEditorStore((s) => s.activeId);
  const headerBinding = useRef({ activeId, currentDatabase });
  headerBinding.current = { activeId, currentDatabase };
  useEffect(() => {
    const editor = useEditorStore.getState();
    if (editor.activeId) editor.setBinding(editor.activeId, { connectionId: activeId, database: currentDatabase });
  }, [activeId, currentDatabase]);
  useEffect(() => {
    const tab = useEditorStore.getState().tabs.find((t) => t.id === activeTabId);
    if (!tab) return;
    const header = headerBinding.current;
    const { connectionId, database } = tab;
    if (!connectionId) {
      // A tab that never had a connection adopts the current one.
      useEditorStore.getState().setBinding(tab.id, { connectionId: header.activeId, database: header.currentDatabase });
      return;
    }
    const connections = useConnectionStore.getState();
    if (connectionId !== header.activeId) {
      if (!connections.connections.some((c) => c.profile.id === connectionId)) return;
      void connections.connect(connectionId).then((ok) => {
        if (ok && database) useSchemaStore.getState().setCurrentDatabase(connectionId, database);
      });
    } else if (database && database !== header.currentDatabase) {
      useSchemaStore.getState().setCurrentDatabase(connectionId, database);
    }
  }, [activeTabId]);

  const disconnect = () => void conn.disconnect();
  const refresh = () => {
    if (activeId) {
      forget(activeId);
      forgetAccess(activeId);
    }
    void conn.load();
  };

  return (
    <div className="flex h-full flex-col">
      <Header
        version={info.version.version}
        theme={config.appearance.theme}
        onThemeChange={onThemeChange}
        onOpenSettings={openSettings}
        panels={visible}
        onTogglePanel={togglePanel}
        databaseControls={
          <DatabaseSelector
            databases={activeId ? schema.databases[activeId] : null}
            current={currentDatabase}
            onOpen={() => activeId && void schema.loadDatabases(activeId)}
            onSelect={(db) => activeId && setCurrentDatabase(activeId, db)}
          />
        }
        connectionControls={
          <ConnectionSelector
            connections={conn.connections}
            activeId={conn.activeId}
            connectingId={conn.connectingId}
            connectError={conn.connectError}
            onConnect={connect}
            onDisconnect={disconnect}
            onNew={dialog.openNew}
            onEdit={dialog.openEdit}
          />
        }
      />

      <ResizablePanelGroup
        orientation="horizontal"
        className="min-h-0 flex-1"
        defaultLayout={mainLayout.defaultLayout}
        onLayoutChanged={mainLayout.onLayoutChanged}
      >
        <ResizablePanel
          id="explorer"
          panelRef={refs.explorer}
          defaultSize={260}
          groupResizeBehavior="preserve-pixel-size"
          minSize={180}
          maxSize="40"
          collapsible
          collapsedSize={0}
          onResize={trackVisibility("explorer")}
        >
          <ExplorerPanel
            connections={conn.connections}
            activeId={conn.activeId}
            connectingId={conn.connectingId}
            loading={conn.status === "loading"}
            loadError={conn.loadError}
            onRefresh={refresh}
            onNew={dialog.openNew}
            onEdit={dialog.openEdit}
            onDelete={dialog.confirmDelete}
            onConnect={connect}
            onDisconnect={disconnect}
            renderSchema={(id, filter) => <SchemaTree connectionId={id} filter={filter} />}
          />
        </ResizablePanel>
        <ResizableHandle />

        <ResizablePanel id="center" minSize="30">
          <ResizablePanelGroup
            orientation="vertical"
            defaultLayout={centerLayout.defaultLayout}
            onLayoutChanged={centerLayout.onLayoutChanged}
          >
            <ResizablePanel id="workspace" minSize={160}>
              <EditorArea
                config={config}
                binding={{ connectionId: activeId, database: currentDatabase }}
                connectionName={(id) => conn.connections.find((c) => c.profile.id === id)?.profile.name ?? null}
                // Execution from the editor is wired up with the results grid (issue #30).
                onCommand={() => {}}
                renderWelcome={(actions) => (
                  <WelcomePage
                    warnings={info.startupWarnings ?? []}
                    connectedTo={active && conn.activeServer ? active.profile.name : null}
                    onNewConnection={dialog.openNew}
                    onNewQuery={actions.newQuery}
                    onOpenFile={actions.openFile}
                  />
                )}
              />
            </ResizablePanel>
            <ResizableHandle />
            <ResizablePanel
              id="bottom"
              panelRef={refs.bottom}
              defaultSize="32"
              minSize={120}
              collapsible
              collapsedSize={0}
              onResize={trackVisibility("bottom")}
            >
              <BottomPanel />
            </ResizablePanel>
          </ResizablePanelGroup>
        </ResizablePanel>

        <ResizableHandle />
        <ResizablePanel
          id="analysis"
          panelRef={refs.analysis}
          defaultSize={300}
          groupResizeBehavior="preserve-pixel-size"
          minSize={220}
          maxSize="45"
          collapsible
          collapsedSize={0}
          onResize={trackVisibility("analysis")}
        >
          <AnalysisPanel />
        </ResizablePanel>
      </ResizablePanelGroup>

      <StatusBar
        config={config}
        onOpenSettings={openSettings}
        connection={
          active && conn.activeServer
            ? { name: active.profile.name, server: { ...conn.activeServer, database: currentDatabase ?? conn.activeServer.database } }
            : null
        }
        access={access}
        onRecheckAccess={() => activeId && currentDatabase && void checkAccess(activeId, currentDatabase, true)}
      />

      <ConnectionDialog
        open={dialog.open}
        onOpenChange={(open) => !open && dialog.close()}
        editing={dialog.editing}
        onSave={conn.save}
        onTest={backend.testConnection}
      />
      <DeleteConnectionDialog
        target={dialog.deleteTarget}
        onCancel={() => dialog.confirmDelete(null)}
        onConfirm={conn.remove}
      />

      <SettingsDialog
        open={settingsOpen}
        onOpenChange={setSettingsOpen}
        config={config}
        info={info}
        onSave={onSaveConfig}
        onLoadDefaults={onLoadDefaults}
        onOpenLogFolder={onOpenLogFolder}
      />
    </div>
  );
}
