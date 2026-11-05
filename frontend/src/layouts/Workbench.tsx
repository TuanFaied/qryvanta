import { FileText } from "lucide-react";
import { useCallback, useState } from "react";
import { useDefaultLayout, usePanelRef, type PanelImperativeHandle, type PanelSize } from "react-resizable-panels";
import { Header } from "@/components/shell/Header";
import { StatusBar } from "@/components/shell/StatusBar";
import { ResizableHandle, ResizablePanel, ResizablePanelGroup } from "@/components/ui/resizable";
import { AnalysisPanel } from "@/features/analysis/AnalysisPanel";
import { ConnectionDialog } from "@/features/connections/ConnectionDialog";
import { useConnectionDialog } from "@/features/connections/connection-dialog-store";
import { ConnectionSelector } from "@/features/connections/ConnectionSelector";
import { DeleteConnectionDialog } from "@/features/connections/DeleteConnectionDialog";
import { ExplorerPanel } from "@/features/explorer/ExplorerPanel";
import { SettingsDialog } from "@/features/settings/SettingsDialog";
import { BottomPanel } from "@/features/workspace/BottomPanel";
import { WelcomePage } from "@/features/workspace/WelcomePage";
import { backend, type AppInfo, type Config, type SaveResult, type Theme } from "@/services/backend";
import { useConnectionStore } from "@/stores/connection-store";
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
  const disconnect = () => void conn.disconnect();

  return (
    <div className="flex h-full flex-col">
      <Header
        version={info.version.version}
        theme={config.appearance.theme}
        onThemeChange={onThemeChange}
        onOpenSettings={openSettings}
        panels={visible}
        onTogglePanel={togglePanel}
        database={conn.activeServer?.database ?? null}
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
            onRefresh={() => void conn.load()}
            onNew={dialog.openNew}
            onEdit={dialog.openEdit}
            onDelete={dialog.confirmDelete}
            onConnect={connect}
            onDisconnect={disconnect}
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
              <main className="flex h-full flex-col" aria-label="Workspace">
                <div className="flex h-8 shrink-0 items-end border-b bg-panel" role="tablist" aria-label="Open documents">
                  <div
                    role="tab"
                    aria-selected="true"
                    className="flex h-8 items-center gap-1.5 border-r border-t-2 border-t-primary bg-editor px-3 text-xs"
                  >
                    <FileText className="size-3.5 text-muted-foreground" />
                    Welcome
                  </div>
                </div>
                <div className="min-h-0 flex-1">
                  <WelcomePage
                    warnings={info.startupWarnings ?? []}
                    connectedTo={active && conn.activeServer ? active.profile.name : null}
                    onNewConnection={dialog.openNew}
                  />
                </div>
              </main>
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
        connection={active && conn.activeServer ? { name: active.profile.name, server: conn.activeServer } : null}
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
