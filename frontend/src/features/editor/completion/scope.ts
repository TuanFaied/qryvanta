import { backend, type Scope } from "@/services/backend";

/** How long completion waits for the parser's scope before using the heuristic alone (ms). */
export const SCOPE_BUDGET_MS = 400;

/**
 * Asks the parser what the query at offset can refer to. Resolves to null (use the heuristic)
 * when the parser is slow, unavailable or the text does not parse; never rejects.
 */
export async function scopeFor(text: string, offset: number, signal: AbortSignal, budgetMs = SCOPE_BUDGET_MS): Promise<Scope | null> {
  let timer: ReturnType<typeof setTimeout> | undefined;
  const timeout = new Promise<null>((resolve) => {
    timer = setTimeout(() => resolve(null), budgetMs);
  });
  try {
    const scope = await Promise.race([backend.completionScope(text, offset, signal).catch(() => null), timeout]);
    return scope?.found ? scope : null;
  } finally {
    clearTimeout(timer);
  }
}
