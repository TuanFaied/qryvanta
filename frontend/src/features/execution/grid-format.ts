import type { ResultColumn, Value } from "@/services/backend";

/** Text for a cell. NULL is the caller's to style; this returns "NULL" for copy and fallbacks. */
export function formatValue(value: Value): string {
  if (value === null || value === undefined) return "NULL";
  if (typeof value === "boolean") return value ? "1" : "0"; // bit, as SQL Server shows it
  return String(value);
}

export function isNull(value: Value): boolean {
  return value === null || value === undefined;
}

const numericTypes = new Set([
  "TINYINT",
  "SMALLINT",
  "INT",
  "BIGINT",
  "DECIMAL",
  "NUMERIC",
  "MONEY",
  "SMALLMONEY",
  "FLOAT",
  "REAL",
  "BIT",
]);

/** Numbers are right-aligned, as in SSMS, so magnitudes line up. */
export function isNumericColumn(column: ResultColumn): boolean {
  return numericTypes.has(column.type.toUpperCase());
}

/** Header tooltip: the SQL type and nullability. */
export function describeColumn(column: ResultColumn): string {
  return `${column.name || "(no column name)"}: ${column.type || "unknown type"}, ${column.nullable ? "NULL" : "NOT NULL"}`;
}

/**
 * One TSV field. Excel and most tools read tab-separated text, quoting fields that contain a
 * tab, a line break or a quote (quotes doubled), so values with those characters survive a paste.
 */
function tsvField(text: string): string {
  return /[\t\r\n"]/.test(text) ? `"${text.replace(/"/g, '""')}"` : text;
}

export interface TsvOptions {
  /** Include a header line with the column names. */
  headers?: boolean;
}

/** Tab-separated text for the given columns of the given rows, lines separated by CRLF. */
export function toTsv(columns: ResultColumn[], rows: Value[][], colIndexes: number[], options: TsvOptions = {}): string {
  const lines: string[] = [];
  if (options.headers) lines.push(colIndexes.map((c) => tsvField(columns[c]?.name ?? "")).join("\t"));
  for (const row of rows) lines.push(colIndexes.map((c) => tsvField(formatValue(row[c]))).join("\t"));
  return lines.join("\r\n");
}

/** A rectangular cell range, inclusive, in data coordinates. */
export interface CellRange {
  anchor: { row: number; col: number };
  focus: { row: number; col: number };
}

export function normalize(range: CellRange) {
  return {
    top: Math.min(range.anchor.row, range.focus.row),
    bottom: Math.max(range.anchor.row, range.focus.row),
    left: Math.min(range.anchor.col, range.focus.col),
    right: Math.max(range.anchor.col, range.focus.col),
  };
}

export function inRange(range: CellRange | null, row: number, col: number): boolean {
  if (!range) return false;
  const r = normalize(range);
  return row >= r.top && row <= r.bottom && col >= r.left && col <= r.right;
}

/** TSV for the selected rectangle. */
export function rangeToTsv(columns: ResultColumn[], rows: Value[][], range: CellRange, options: TsvOptions = {}): string {
  const r = normalize(range);
  const cols: number[] = [];
  for (let c = r.left; c <= r.right; c++) cols.push(c);
  return toTsv(columns, rows.slice(r.top, r.bottom + 1), cols, options);
}

/** Initial column width in pixels, from the header and the first rows. */
export function initialWidth(column: ResultColumn, rows: Value[][], index: number): number {
  let chars = Math.max(column.name.length, 4);
  for (let i = 0; i < Math.min(rows.length, 50); i++) {
    chars = Math.max(chars, formatValue(rows[i]?.[index]).split(/\r?\n/)[0]!.length);
  }
  return Math.min(Math.max(chars * 7 + 20, 64), 420);
}
