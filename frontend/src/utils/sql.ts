/** Quotes a SQL Server identifier like QUOTENAME: brackets, with ] doubled. */
export function quoteIdent(name: string): string {
  return `[${name.replaceAll("]", "]]")}]`;
}

/** [schema].[name] */
export function qualifiedName(schema: string, name: string): string {
  return `${quoteIdent(schema)}.${quoteIdent(name)}`;
}

/** A SELECT TOP (100) statement for a table or view, with explicit columns when known. */
export function selectTop100(schema: string, name: string, columns: string[] = []): string {
  const cols = columns.length > 0 ? columns.map((c) => `    ${quoteIdent(c)}`).join(",\n") : "    *";
  return `SELECT TOP (100)\n${cols}\nFROM ${qualifiedName(schema, name)};`;
}
