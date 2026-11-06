import { AccessLevel, type WriteAccess } from "@/services/backend";

export type AccessTone = "ok" | "warn" | "danger" | "muted";

/** Short label, tone and explanation for a write-access result. */
export function describeAccess(a: WriteAccess | null | undefined): { label: string; tone: AccessTone; detail: string } {
  if (!a) return { label: "Checking permissions…", tone: "muted", detail: "Checking what this login can change." };
  switch (a.level) {
    case AccessLevel.AccessReadOnly:
      return {
        label: "Read-only login",
        tone: "ok",
        detail: `No permission to change data was found in ${a.database}. This is the safest way to debug queries.`,
      };
    case AccessLevel.AccessMayWrite:
      return {
        label: "May modify via procedures",
        tone: "warn",
        detail: `This login cannot change tables directly in ${a.database}, but it can execute code that might.`,
      };
    case AccessLevel.AccessCanWrite:
      return {
        label: "Login can modify data",
        tone: "danger",
        detail: `This login can change data or schema in ${a.database}. Qryvanta's read-only mode helps, but only a read-only login guarantees nothing is changed.`,
      };
    default:
      return { label: "Permissions unknown", tone: "muted", detail: `The permission check did not complete${a.error ? `: ${a.error}` : "."}` };
  }
}
