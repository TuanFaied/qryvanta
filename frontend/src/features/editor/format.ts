import { format } from "sql-formatter";
import { KeywordCase, type Formatting } from "@/services/backend";

export type FormatResult = { ok: true; text: string } | { ok: false; error: string };

/**
 * Formats T-SQL with the user's formatting settings. sql-formatter only re-lays out tokens,
 * so the meaning of the batch does not change; text it cannot tokenize is reported, not
 * guessed at.
 */
export function formatSql(text: string, options: Formatting): FormatResult {
  if (text.trim() === "") return { ok: true, text };
  const casing = options.keywordCase === KeywordCase.KeywordLower ? "lower" : options.keywordCase === KeywordCase.KeywordPreserve ? "preserve" : "upper";
  try {
    const formatted = format(text, {
      language: "transactsql",
      keywordCase: casing,
      dataTypeCase: casing,
      functionCase: casing,
      tabWidth: options.indentSize,
      useTabs: options.useTabs,
      linesBetweenQueries: 1,
    });
    // Keep a trailing newline if the original had one, so formatting does not create a diff
    // on the last line of a saved file.
    return { ok: true, text: /\r?\n$/.test(text) && !formatted.endsWith("\n") ? formatted + "\n" : formatted };
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err);
    return { ok: false, error: `Could not format: ${message.split("\n")[0]}` };
  }
}
