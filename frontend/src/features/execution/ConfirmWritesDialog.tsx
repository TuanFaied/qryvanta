import { AlertTriangle } from "lucide-react";
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
import { AccessLevel } from "@/services/backend";
import type { PendingRun } from "./useExecution";

interface ConfirmWritesDialogProps {
  pending: PendingRun | null;
  connectionName: string;
  onConfirm: () => void;
  onCancel: () => void;
}

/** Asked once per connection and session when read-only mode is off and the login may write. */
export function ConfirmWritesDialog({ pending, connectionName, onConfirm, onCancel }: ConfirmWritesDialogProps) {
  const access = pending?.access ?? null;
  const database = pending?.request.database;
  let why: string;
  if (!access || access.level === AccessLevel.AccessUnknown) {
    why = `Qryvanta could not check whether this login can modify data${database ? ` in ${database}` : ""}.`;
  } else if (access.level === AccessLevel.AccessMayWrite) {
    why = `This login may modify data in ${access.database} through procedures it can execute.`;
  } else {
    why = `This login can modify data in ${access.database}.`;
  }
  const reasons = access?.reasons ?? [];

  return (
    <AlertDialog open={pending !== null} onOpenChange={(o) => !o && onCancel()}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle className="flex items-center gap-2">
            <AlertTriangle className="size-4 text-warning" />
            Run with read-only mode off?
          </AlertDialogTitle>
          <AlertDialogDescription asChild>
            <div className="space-y-2 text-sm">
              <p>
                {why} Read-only mode is off, so changes made by queries on <strong>{connectionName}</strong> will be committed.
              </p>
              {reasons.length > 0 && (
                <ul className="list-disc pl-5 text-xs">
                  {reasons.slice(0, 4).map((r) => (
                    <li key={r.text}>{r.text}</li>
                  ))}
                </ul>
              )}
              <p className="text-xs">You will not be asked again for this connection until Qryvanta restarts.</p>
            </div>
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction onClick={onConfirm}>Run query</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
