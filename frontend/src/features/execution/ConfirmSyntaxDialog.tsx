import { CircleX } from "lucide-react";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import type { PendingSyntax } from "./useExecution";

interface ConfirmSyntaxDialogProps {
  pending: PendingSyntax | null;
  onRunAnyway: () => void;
  onCancel: () => void;
}

/** Asked before running SQL in which the editor's syntax check found errors. */
export function ConfirmSyntaxDialog({ pending, onRunAnyway, onCancel }: ConfirmSyntaxDialogProps) {
  const diagnostics = pending?.diagnostics ?? [];
  const n = diagnostics.length;
  return (
    <AlertDialog open={pending !== null} onOpenChange={(o) => !o && onCancel()}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle className="flex items-center gap-2">
            <CircleX className="size-4 text-destructive" />
            {n === 1 ? "The query has a syntax error" : `The query has ${n} syntax errors`}
          </AlertDialogTitle>
          <AlertDialogDescription asChild>
            <div className="space-y-2 text-sm">
              <ul className="space-y-1 font-mono text-xs">
                {diagnostics.slice(0, 3).map((d) => (
                  <li key={`${d.offset}-${d.number}`}>
                    Line {d.line}, column {d.column}: {d.message}
                  </li>
                ))}
                {n > 3 && <li>and {n - 3} more</li>}
              </ul>
              <p>SQL Server will most likely reject the batch that contains it. Batches without errors still run.</p>
            </div>
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Fix it first</AlertDialogCancel>
          <AlertDialogAction onClick={onRunAnyway}>Run anyway</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
