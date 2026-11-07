import type { Config } from "@/services/backend";

/** Numeric settings edited as text, keyed by their JSON path in Config. */
export const numericFields = [
  { path: "appearance.editorFontSize", label: "Editor font size", unit: "px" },
  { path: "execution.timeoutSeconds", label: "Query timeout", unit: "seconds" },
  { path: "execution.maxRows", label: "Maximum rows", unit: "rows" },
  { path: "execution.maxResultMB", label: "Maximum result size", unit: "MB" },
  { path: "execution.connectTimeoutSeconds", label: "Connection timeout", unit: "seconds" },
  { path: "execution.lockTimeoutSeconds", label: "Lock wait limit (read-only mode)", unit: "seconds" },
  { path: "execution.probeTimeoutSeconds", label: "Probe query timeout", unit: "seconds" },
  { path: "execution.maxProbesPerRun", label: "Maximum probe queries per run", unit: "queries" },
  { path: "history.retentionDays", label: "Keep history for", unit: "days" },
] as const;

export type NumericPath = (typeof numericFields)[number]["path"];

export type NumericDraft = Record<NumericPath, string>;

function getNumber(config: Config, path: NumericPath): number {
  const [section, key] = path.split(".") as [keyof Config, string];
  return (config[section] as unknown as Record<string, number>)[key] ?? 0;
}

/** Text values for every numeric field. */
export function toDraft(config: Config): NumericDraft {
  const draft = {} as NumericDraft;
  for (const f of numericFields) draft[f.path] = String(getNumber(config, f.path));
  return draft;
}

/**
 * Applies text values to a copy of config. Values that are not whole numbers are reported
 * as errors; range checks are done by the backend, which owns the rules.
 */
export function applyDraft(config: Config, draft: NumericDraft): { config: Config; errors: Record<string, string> } {
  const next: Config = structuredClone(config);
  const errors: Record<string, string> = {};
  for (const f of numericFields) {
    const text = draft[f.path].trim();
    if (!/^-?\d+$/.test(text)) {
      errors[f.path] = "must be a whole number";
      continue;
    }
    const [section, key] = f.path.split(".") as [keyof Config, string];
    (next[section] as unknown as Record<string, number>)[key] = Number.parseInt(text, 10);
  }
  return { config: next, errors };
}
