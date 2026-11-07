import { ObjectKind, type Column, type SchemaObject } from "@/services/backend";
import { detectContext, findTableRefs, quoteIfNeeded, sameName, type TableRef } from "./context";

export type SuggestionKind = "table" | "view" | "function" | "column" | "alias" | "keyword" | "builtin";

export interface Suggestion {
  label: string;
  kind: SuggestionKind;
  insertText: string;
  /** Short text next to the label: type, key information, object kind. */
  detail: string;
  /** Longer text in the details pane. */
  documentation?: string;
  sortText: string;
  /** insertText is a Monaco snippet ("COUNT($0)"). */
  snippet?: boolean;
}

/** Metadata the suggestions read; backed by the schema store (cached results first). */
export interface MetadataSource {
  /** Objects of a database, or undefined when not loaded (yet). */
  objects: (database: string) => SchemaObject[] | undefined;
  columns: (database: string, schema: string, name: string) => Column[] | undefined;
  /** Starts loading; resolves when done. */
  loadObjects: (database: string) => Promise<void>;
  loadColumns: (database: string, schema: string, name: string) => Promise<void>;
}

export interface SuggestRequest {
  /** Statement text up to the cursor. */
  textBefore: string;
  /** The whole statement around the cursor, for table references. */
  statement: string;
  /** The tab's current database; null without a connection. */
  database: string | null;
  source: MetadataSource | null;
  /** How long to wait for metadata that is not cached yet (ms). */
  budgetMs?: number;
}

export interface SuggestResult {
  items: Suggestion[];
  /** Some metadata was still loading; asking again later may return more. */
  incomplete: boolean;
}

const keywords = `SELECT FROM WHERE JOIN INNER LEFT RIGHT FULL OUTER CROSS APPLY ON AND OR NOT IN EXISTS BETWEEN LIKE IS NULL
AS DISTINCT TOP PERCENT WITH TIES ORDER BY GROUP HAVING UNION ALL EXCEPT INTERSECT CASE WHEN THEN ELSE END ASC DESC
OFFSET ROWS FETCH NEXT ONLY OVER PARTITION INSERT INTO VALUES UPDATE SET DELETE MERGE OUTPUT DECLARE BEGIN COMMIT
ROLLBACK TRANSACTION IF WHILE RETURN PRINT EXEC USE GO NOLOCK`.split(/\s+/);

const functions = `COUNT SUM AVG MIN MAX COUNT_BIG STRING_AGG COALESCE ISNULL NULLIF IIF CHOOSE CAST CONVERT TRY_CAST TRY_CONVERT
DATEADD DATEDIFF DATEDIFF_BIG DATEPART DATENAME DATEFROMPARTS EOMONTH GETDATE GETUTCDATE SYSDATETIME SYSDATETIMEOFFSET
YEAR MONTH DAY LEN DATALENGTH LEFT RIGHT SUBSTRING CHARINDEX PATINDEX REPLACE LTRIM RTRIM TRIM UPPER LOWER CONCAT
CONCAT_WS FORMAT STRING_SPLIT ROW_NUMBER RANK DENSE_RANK NTILE LAG LEAD FIRST_VALUE LAST_VALUE ROUND ABS CEILING FLOOR
NEWID OBJECT_ID DB_NAME SCHEMA_NAME`.split(/\s+/);

/** Waits for a load, but never longer than ms; completion must not wait on a slow server. */
async function within(p: Promise<void>, ms: number): Promise<void> {
  await Promise.race([p.catch(() => {}), new Promise<void>((r) => setTimeout(r, ms))]);
}

function generalItems(): Suggestion[] {
  return [
    ...keywords.map((k): Suggestion => ({ label: k, kind: "keyword", insertText: k, detail: "keyword", sortText: `4${k}` })),
    ...functions.map(
      (f): Suggestion => ({ label: f, kind: "builtin", insertText: `${f}($0)`, detail: "function", sortText: `5${f}`, snippet: true }),
    ),
  ];
}

const isQueryable = (o: SchemaObject) => o.kind === ObjectKind.KindTable || o.kind === ObjectKind.KindView || o.kind === ObjectKind.KindFunction;

function objectItem(o: SchemaObject, qualify: boolean): Suggestion {
  const kind: SuggestionKind = o.kind === ObjectKind.KindView ? "view" : o.kind === ObjectKind.KindFunction ? "function" : "table";
  return {
    label: qualify ? `${o.schema}.${o.name}` : o.name,
    kind,
    insertText: qualify ? `${quoteIfNeeded(o.schema)}.${quoteIfNeeded(o.name)}` : quoteIfNeeded(o.name),
    detail: kind,
    // Tables before views before functions; dbo first, as it is the usual default schema.
    sortText: `${kind === "table" ? 1 : kind === "view" ? 2 : 3}${o.schema === "dbo" ? 0 : 1}${o.schema}.${o.name}`,
  };
}

/** "int · PK", "nvarchar(100) · FK → dbo.Customers.CustomerID", like the explorer tree. */
export function columnDetail(c: Column): string {
  const parts = [c.dataType];
  if (c.primaryKey) parts.push("PK");
  if (c.references) parts.push(`FK → ${c.references}`);
  return parts.join(" · ");
}

function columnItem(c: Column, owner: string | null, rank: string): Suggestion {
  const flags = [c.nullable ? "NULL" : "NOT NULL", c.identity && "identity", c.computed && "computed"].filter(Boolean).join(", ");
  return {
    label: c.name,
    kind: "column",
    insertText: quoteIfNeeded(c.name),
    detail: owner ? `${owner} · ${columnDetail(c)}` : columnDetail(c),
    documentation: `${c.name} ${c.dataType} ${flags}${c.references ? `\nReferences ${c.references}` : ""}`,
    sortText: `${rank}${String(c.ordinal).padStart(4, "0")}`,
  };
}

/** Finds the object a reference names in a database's object list: exact schema, else dbo, else the only match. */
function resolve(objects: SchemaObject[], ref: Pick<TableRef, "schema" | "name">): SchemaObject | undefined {
  const candidates = objects.filter((o) => isQueryable(o) && sameName(o.name, ref.name));
  if (ref.schema) return candidates.find((o) => sameName(o.schema, ref.schema));
  return candidates.find((o) => o.schema === "dbo") ?? (candidates.length === 1 ? candidates[0] : undefined);
}

/**
 * Builds suggestions for the cursor position. Metadata is taken from the cache; anything
 * missing is requested and waited for up to budgetMs, then the result is returned without it
 * (incomplete) so typing never waits on the server.
 */
export async function computeSuggestions(req: SuggestRequest): Promise<SuggestResult> {
  const ctx = detectContext(req.textBefore);
  if (ctx.kind === "none") return { items: [], incomplete: false };

  const { source, database } = req;
  const budget = req.budgetMs ?? 400;
  let incomplete = false;

  const objectsOf = async (db: string): Promise<SchemaObject[]> => {
    if (!source) return [];
    let objs = source.objects(db);
    if (!objs) {
      await within(source.loadObjects(db), budget);
      objs = source.objects(db);
    }
    if (!objs) incomplete = true;
    return objs ?? [];
  };
  const columnsOf = async (db: string, o: SchemaObject): Promise<Column[]> => {
    if (!source) return [];
    let cols = source.columns(db, o.schema, o.name);
    if (!cols) {
      await within(source.loadColumns(db, o.schema, o.name), budget);
      cols = source.columns(db, o.schema, o.name);
    }
    if (!cols) incomplete = true;
    return cols ?? [];
  };

  if (!database || !source) {
    return { items: ctx.kind === "general" ? generalItems() : [], incomplete: false };
  }

  if (ctx.kind === "object") {
    const objs = await objectsOf(database);
    return { items: objs.filter(isQueryable).map((o) => objectItem(o, true)), incomplete };
  }

  const refs = findTableRefs(req.statement);

  if (ctx.kind === "member") {
    const q = ctx.qualifier;
    if (q.length === 1) {
      // alias. → columns; schema. → that schema's objects; table. → columns. Aliases win, then
      // schemas, because "FROM dbo." is itself read as a reference to a table named dbo.
      const columnsOfRef = async (ref: TableRef) => {
        const db = ref.database ?? database;
        const obj = resolve(await objectsOf(db), ref);
        return { items: obj ? (await columnsOf(db, obj)).map((c) => columnItem(c, null, "0")) : [], incomplete };
      };
      const aliased = refs.find((r) => sameName(r.alias, q[0]));
      if (aliased) return columnsOfRef(aliased);
      const objs = await objectsOf(database);
      const inSchema = objs.filter((o) => isQueryable(o) && sameName(o.schema, q[0]));
      if (inSchema.length > 0) return { items: inSchema.map((o) => objectItem(o, false)), incomplete };
      const named = refs.find((r) => !r.alias && sameName(r.name, q[0]));
      if (named) return columnsOfRef(named);
      // A table that is not in the statement yet: dbo.Orders typed in SELECT before FROM.
      const obj = resolve(objs, { schema: null, name: q[0]! });
      return { items: obj ? (await columnsOf(database, obj)).map((c) => columnItem(c, null, "0")) : [], incomplete };
    }
    if (q.length === 2) {
      // schema.table. → columns, or database.schema. → objects of that database's schema.
      const objs = await objectsOf(database);
      const obj = resolve(objs, { schema: q[0]!, name: q[1]! });
      if (obj) return { items: (await columnsOf(database, obj)).map((c) => columnItem(c, null, "0")), incomplete };
      const other = await objectsOf(q[0]!);
      return { items: other.filter((o) => isQueryable(o) && sameName(o.schema, q[1])).map((o) => objectItem(o, false)), incomplete };
    }
    // database.schema.table.
    const obj = resolve(await objectsOf(q[0]!), { schema: q[1]!, name: q[2]! });
    return { items: obj ? (await columnsOf(q[0]!, obj)).map((c) => columnItem(c, null, "0")) : [], incomplete };
  }

  // General: columns of the referenced tables, the aliases, then keywords and functions.
  const items: Suggestion[] = [];
  const seen = new Set<string>();
  for (const ref of refs) {
    const db = ref.database ?? database;
    const obj = resolve(await objectsOf(db), ref);
    const owner = ref.alias ?? ref.name;
    items.push({
      label: owner,
      kind: "alias",
      insertText: quoteIfNeeded(owner),
      detail: ref.alias ? `alias of ${ref.schema ? `${ref.schema}.` : ""}${ref.name}` : "table",
      sortText: `2${owner}`,
    });
    if (!obj) continue;
    for (const c of await columnsOf(db, obj)) {
      // A column present in several tables is listed once per table, labelled with its owner.
      const key = `${owner}.${c.name}`;
      if (seen.has(key)) continue;
      seen.add(key);
      items.push(columnItem(c, owner, "1"));
    }
  }
  items.push(...generalItems());
  return { items, incomplete };
}
