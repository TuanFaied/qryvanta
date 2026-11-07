import {
  Bug,
  Check,
  Monitor,
  Moon,
  PanelBottom,
  PanelLeft,
  PanelRight,
  Play,
  Settings,
  Square,
  Sun,
} from "lucide-react";
import type { ReactNode } from "react";
import { Logo } from "@/components/brand/Logo";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Separator } from "@/components/ui/separator";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { IconButton } from "@/components/shell/IconButton";
import { Theme } from "@/services/backend";
import type { PanelId } from "@/layouts/panels";

const themeOptions = [
  { value: Theme.ThemeDark, label: "Dark", icon: Moon },
  { value: Theme.ThemeLight, label: "Light", icon: Sun },
  { value: Theme.ThemeSystem, label: "System", icon: Monitor },
] as const;

interface HeaderProps {
  version: string;
  theme: Theme;
  onThemeChange: (theme: Theme) => void;
  onOpenSettings: () => void;
  panels: Record<PanelId, boolean>;
  onTogglePanel: (id: PanelId) => void;
  /** The connection selector, provided by the connections feature. */
  connectionControls: ReactNode;
  /** The current-database selector, provided by the explorer feature. */
  databaseControls: ReactNode;
  execution: ExecutionControls;
}

export interface ExecutionControls {
  /** Why Execute is unavailable (no tab, no connection, already running), or null. */
  blockedReason: string | null;
  running: boolean;
  onExecute: () => void;
  onStop: () => void;
}

export function Header({
  version,
  theme,
  onThemeChange,
  onOpenSettings,
  panels,
  onTogglePanel,
  connectionControls,
  databaseControls,
  execution,
}: HeaderProps) {
  const ThemeIcon = themeOptions.find((t) => t.value === theme)?.icon ?? Moon;

  return (
    <header className="flex h-10 shrink-0 items-center gap-1 border-b bg-chrome px-2">
      <Logo className="px-1.5" />
      <Separator orientation="vertical" className="mx-1.5 !h-5" />

      {connectionControls}
      {databaseControls}

      <Separator orientation="vertical" className="mx-1.5 !h-5" />

      <div className="flex items-center gap-0.5" role="toolbar" aria-label="Execution">
        <IconButton
          icon={Play}
          label="Execute"
          hint={execution.blockedReason ? `Ctrl+Enter · ${execution.blockedReason}` : "Ctrl+Enter · runs the selection, or the whole tab"}
          disabled={!!execution.blockedReason}
          onClick={execution.onExecute}
          className="text-success"
        />
        <IconButton icon={Bug} label="Execute with debugging" hint="Ctrl+Shift+Enter · arrives with the diagnostic engine (Phase 5)" disabled />
        <IconButton
          icon={Square}
          label="Stop execution"
          hint={execution.running ? "Esc · cancels the query on the server" : "Esc · no query is running"}
          disabled={!execution.running}
          onClick={execution.onStop}
        />
      </div>

      <div className="flex-1" />

      <div className="flex items-center gap-0.5" role="toolbar" aria-label="Layout">
        <IconButton icon={PanelLeft} label="Toggle explorer" active={panels.explorer} onClick={() => onTogglePanel("explorer")} />
        <IconButton icon={PanelBottom} label="Toggle bottom panel" active={panels.bottom} onClick={() => onTogglePanel("bottom")} />
        <IconButton icon={PanelRight} label="Toggle analysis panel" active={panels.analysis} onClick={() => onTogglePanel("analysis")} />
      </div>

      <Separator orientation="vertical" className="mx-1.5 !h-5" />

      <DropdownMenu>
        <Tooltip>
          <TooltipTrigger asChild>
            <DropdownMenuTrigger asChild>
              <Button variant="ghost" size="icon" aria-label="Theme" className="size-7 text-muted-foreground hover:text-foreground">
                <ThemeIcon className="size-4" strokeWidth={1.75} />
              </Button>
            </DropdownMenuTrigger>
          </TooltipTrigger>
          <TooltipContent side="bottom">Theme</TooltipContent>
        </Tooltip>
        <DropdownMenuContent align="end" className="w-36">
          <DropdownMenuLabel className="text-xs text-muted-foreground">Theme</DropdownMenuLabel>
          {themeOptions.map((opt) => (
            <DropdownMenuItem key={opt.value} onSelect={() => onThemeChange(opt.value)} className="text-xs">
              <opt.icon className="size-3.5" />
              {opt.label}
              {opt.value === theme && <Check className="ml-auto size-3.5" aria-label="selected" />}
            </DropdownMenuItem>
          ))}
        </DropdownMenuContent>
      </DropdownMenu>

      <IconButton icon={Settings} label="Settings" onClick={onOpenSettings} />

      <span className="ml-1 rounded-sm border px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground" title="Application version">
        v{version}
      </span>
    </header>
  );
}
