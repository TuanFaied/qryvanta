import type { LucideIcon } from "lucide-react";
import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

interface EmptyStateProps {
  icon: LucideIcon;
  title: string;
  children?: ReactNode;
  className?: string;
}

export function EmptyState({ icon: Icon, title, children, className }: EmptyStateProps) {
  return (
    <div className={cn("flex h-full flex-col items-center justify-center gap-2 px-6 py-8 text-center", className)}>
      <Icon className="size-6 text-muted-foreground/60" strokeWidth={1.5} aria-hidden="true" />
      <p className="text-[13px] font-medium text-foreground/90">{title}</p>
      {children && <div className="max-w-72 text-xs leading-relaxed text-muted-foreground">{children}</div>}
    </div>
  );
}
