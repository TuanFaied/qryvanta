import { Plus, RefreshCw, Search, Server } from "lucide-react";
import { EmptyState } from "@/components/shell/EmptyState";
import { IconButton } from "@/components/shell/IconButton";
import { PanelHeader } from "@/components/shell/PanelHeader";

const phase2 = "Arrives with connection management (Phase 2)";

export function ExplorerPanel() {
  return (
    <section aria-label="Database explorer" className="flex h-full flex-col bg-panel">
      <PanelHeader
        title="Explorer"
        actions={
          <>
            <IconButton icon={Search} label="Search objects" hint={phase2} disabled />
            <IconButton icon={RefreshCw} label="Refresh" hint={phase2} disabled />
            <IconButton icon={Plus} label="New connection" hint={phase2} disabled />
          </>
        }
      />
      <EmptyState icon={Server} title="No connections">
        Saved SQL Server connections and their databases, schemas, tables, views and stored procedures will be
        listed here.
      </EmptyState>
    </section>
  );
}
