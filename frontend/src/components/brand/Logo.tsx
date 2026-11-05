import { cn } from "@/lib/utils";

/** Qryvanta mark: a lens over a result row, the product's "look inside the query" idea. */
export function LogoMark({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" fill="none" aria-hidden="true" className={cn("size-5", className)}>
      <rect x="2.5" y="4" width="13" height="2.2" rx="1.1" className="fill-muted-foreground/50" />
      <rect x="2.5" y="9" width="9" height="2.2" rx="1.1" className="fill-muted-foreground/50" />
      <rect x="2.5" y="14" width="6" height="2.2" rx="1.1" className="fill-muted-foreground/50" />
      <circle cx="15.5" cy="13.5" r="5" strokeWidth="2.2" className="stroke-primary" />
      <path d="M19 17l3 3" strokeWidth="2.4" strokeLinecap="round" className="stroke-primary" />
    </svg>
  );
}

export function Logo({ className }: { className?: string }) {
  return (
    <div className={cn("flex items-center gap-2", className)}>
      <LogoMark />
      <span className="text-[13px] font-semibold tracking-tight">Qryvanta</span>
    </div>
  );
}
