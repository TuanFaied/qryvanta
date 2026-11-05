import { create } from "zustand";
import type { ConnectionDTO } from "@/services/backend";

interface ConnectionDialogState {
  /** Open with null for a new connection, or a connection to edit. */
  editing: ConnectionDTO | null;
  open: boolean;
  deleteTarget: ConnectionDTO | null;
  openNew: () => void;
  openEdit: (c: ConnectionDTO) => void;
  close: () => void;
  confirmDelete: (c: ConnectionDTO | null) => void;
}

export const useConnectionDialog = create<ConnectionDialogState>((set) => ({
  editing: null,
  open: false,
  deleteTarget: null,
  openNew: () => set({ open: true, editing: null }),
  openEdit: (c) => set({ open: true, editing: c }),
  close: () => set({ open: false }),
  confirmDelete: (c) => set({ deleteTarget: c }),
}));
