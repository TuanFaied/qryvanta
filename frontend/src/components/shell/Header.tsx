import {
  Bug,
  Check,
  ChevronDown,
  Database,
  Monitor,
  Moon,
  PanelBottom,
  PanelLeft,
  PanelRight,
  Play,
  Plug,
  Settings,
  Square,
  Sun,
} from "lucide-react";
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

const notYet = "Available once connections and the editor land (Phase 2–3)";

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
}

export function Header({ version, theme, onThemeChange, onOpenSettings, panels, onTogglePanel }: HeaderProps) {
  const ThemeIcon = themeOptions.find((t) => t.value === theme)?.icon ?? Moon;

  return (
    <header className="flex h-10 shrink-0 items-center gap-1 border-b bg-chrome px-2">
      <Logo className="px-1.5" />
      <Separator orientation="vertical" className="mx-1.5 !h-5" />

      {/* Connection and database context. Real selectors arrive in Phase 2. */}
      <Tooltip>
        <TooltipTrigger asChild>
          <span tabIndex={0} className="inline-flex">
            <Button variant="ghost" size="sm" disabled className="h-7 gap-1.5 px-2 text-xs font-normal">
              <Plug className="size-3.5" />
              <span className="size-1.5 rounded-full bg-muted-foreground/50" aria-hidden="true" />
              No connection
              <ChevronDown className="size-3 opacity-60" />
            </Button>
          </span>
        </TooltipTrigger>
        <TooltipContent side="bottom">Connection management arrives in Phase 2</TooltipContent>
      </Tooltip>
      <Tooltip>
        <TooltipTrigger asChild>
          <span tabIndex={0} className="inline-flex">
            <Button variant="ghost" size="sm" disabled className="h-7 gap-1.5 px-2 text-xs font-normal">
              <Database className="size-3.5" />
              No database
              <ChevronDown className="size-3 opacity-60" />
            </Button>
          </span>
        </TooltipTrigger>
        <TooltipContent side="bottom">Select a connection first</TooltipContent>
      </Tooltip>

      <Separator orientation="vertical" className="mx-1.5 !h-5" />

      <div className="flex items-center gap-0.5" role="toolbar" aria-label="Execution">
        <IconButton icon={Play} label="Execute" hint={`Ctrl+Enter · ${notYet}`} disabled className="text-success" />
        <IconButton icon={Bug} label="Execute with debugging" hint={`Ctrl+Shift+Enter · ${notYet}`} disabled />
        <IconButton icon={Square} label="Stop execution" hint="Esc" disabled />
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
