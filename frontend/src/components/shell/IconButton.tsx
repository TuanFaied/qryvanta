import type { LucideIcon } from "lucide-react";
import type { ComponentProps } from "react";
import { Button } from "@/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";

interface IconButtonProps extends Omit<ComponentProps<typeof Button>, "children"> {
  icon: LucideIcon;
  label: string;
  /** Extra tooltip text, for example a keyboard shortcut or why the button is disabled. */
  hint?: string;
  active?: boolean;
}

/**
 * Small icon button with an accessible label and tooltip. Disabled buttons are wrapped so the
 * tooltip still explains why they are disabled.
 */
export function IconButton({ icon: Icon, label, hint, active, disabled, className, ...props }: IconButtonProps) {
  const button = (
    <Button
      variant="ghost"
      size="icon"
      aria-label={label}
      aria-pressed={active}
      disabled={disabled}
      className={cn("size-7 text-muted-foreground hover:text-foreground", active && "bg-accent text-foreground", className)}
      {...props}
    >
      <Icon className="size-4" strokeWidth={1.75} />
    </Button>
  );
  return (
    <Tooltip>
      <TooltipTrigger asChild>{disabled ? <span tabIndex={0} className="inline-flex">{button}</span> : button}</TooltipTrigger>
      <TooltipContent side="bottom">
        <span className="font-medium">{label}</span>
        {hint && <span className="ml-1.5 opacity-70">{hint}</span>}
      </TooltipContent>
    </Tooltip>
  );
}
