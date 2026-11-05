import { useState } from "react";
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
import { AuthMethod, type ConnectionDTO } from "@/services/backend";

interface DeleteConnectionDialogProps {
  target: ConnectionDTO | null;
  onCancel: () => void;
  onConfirm: (id: string) => Promise<void>;
}

export function DeleteConnectionDialog({ target, onCancel, onConfirm }: DeleteConnectionDialogProps) {
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const confirm = async () => {
    if (!target) return;
    setBusy(true);
    setError(null);
    try {
      await onConfirm(target.profile.id);
      onCancel();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <AlertDialog open={target !== null} onOpenChange={(open) => !open && onCancel()}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Delete “{target?.profile.name}”?</AlertDialogTitle>
          <AlertDialogDescription className="text-xs">
            The connection is removed from Qryvanta
            {target?.profile.auth === AuthMethod.AuthSQL && target.hasPassword
              ? " and its saved password is deleted from Windows Credential Manager"
              : ""}
            . Nothing on the database server changes.
          </AlertDialogDescription>
        </AlertDialogHeader>
        {error && (
          <p role="alert" className="text-xs text-destructive">
            {error}
          </p>
        )}
        <AlertDialogFooter>
          <AlertDialogCancel className="h-8 text-xs">Cancel</AlertDialogCancel>
          <AlertDialogAction
            className="h-8 bg-destructive text-xs text-white hover:bg-destructive/90"
            disabled={busy}
            onClick={(e) => {
              e.preventDefault();
              void confirm();
            }}
          >
            {busy ? "Deleting…" : "Delete connection"}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
