import { AlertTriangle, Loader2 } from "lucide-react";
import { useEffect } from "react";
import { Button } from "@/components/ui/button";
import { TooltipProvider } from "@/components/ui/tooltip";
import { applyTheme, watchSystemTheme } from "@/features/theme/theme";
import { Workbench } from "@/layouts/Workbench";
import { backend, Theme } from "@/services/backend";
import { useAppStore } from "@/stores/app-store";

export default function App() {
  const { status, error, info, config, load, saveConfig, setTheme } = useAppStore();

  useEffect(() => {
    void load();
  }, [load]);

  const theme = config?.appearance.theme ?? Theme.ThemeDark;
  useEffect(() => {
    applyTheme(theme);
    if (theme !== Theme.ThemeSystem) return;
    return watchSystemTheme(() => applyTheme(theme));
  }, [theme]);

  if (status === "error" || (status === "ready" && (!info || !config))) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-3 p-8 text-center">
        <AlertTriangle className="size-6 text-destructive" />
        <p className="text-sm font-medium">Qryvanta could not start</p>
        <p className="selectable max-w-md text-xs text-muted-foreground">{error ?? "The backend returned no data."}</p>
        <Button size="sm" variant="outline" onClick={() => void load()}>
          Try again
        </Button>
      </div>
    );
  }

  if (status === "loading" || !info || !config) {
    return (
      <div className="flex h-full items-center justify-center" aria-busy="true">
        <Loader2 className="size-5 animate-spin text-muted-foreground" aria-label="Loading" />
      </div>
    );
  }

  return (
    <TooltipProvider delayDuration={400}>
      <Workbench
        info={info}
        config={config}
        onThemeChange={(t) => void setTheme(t)}
        onSaveConfig={saveConfig}
        onLoadDefaults={backend.getDefaultConfig}
        onOpenLogFolder={backend.openLogFolder}
      />
    </TooltipProvider>
  );
}
