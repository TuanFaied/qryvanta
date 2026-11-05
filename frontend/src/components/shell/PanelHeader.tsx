import type { ReactNode } from "react";

export function PanelHeader({ title, actions }: { title: string; actions?: ReactNode }) {
  return (
    <div className="flex h-8 shrink-0 items-center justify-between pr-1 pl-3">
      <h2 className="text-[11px] font-semibold tracking-wider text-muted-foreground uppercase">{title}</h2>
      {actions && <div className="flex items-center">{actions}</div>}
    </div>
  );
}
