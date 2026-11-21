import { describe, expect, it, vi } from "vitest";
import { ObjectKind, type Column, type SchemaObject, type Scope, type ScopeSource } from "@/services/backend";
import { columnDetail, computeSuggestions, type MetadataSource } from "./suggestions";

const objects: SchemaObject[] = [
  { schema: "dbo", name: "Orders", kind: ObjectKind.KindTable },
  { schema: "dbo", name: "Customers", kind: ObjectKind.KindTable },
  { schema: "dbo", name: "Order Details", kind: ObjectKind.KindTable },
  { schema: "dbo", name: "vActiveCustomers", kind: ObjectKind.KindView },
  { schema: "dbo", name: "usp_Report", kind: ObjectKind.KindProcedure },
  { schema: "sales", name: "Orders", kind: ObjectKind.KindTable },
];

const col = (ordinal: number, name: string, extra: Partial<Column> = {}): Column => ({
  ordinal,
  name,
  dataType: "int",
  nullable: false,
  identity: false,
  computed: false,
  primaryKey: false,
  references: "",
  ...extra,
});

const columns: Record<string, Column[]> = {
  "dbo.Orders": [
    col(1, "OrderID", { primaryKey: true, identity: true }),
    col(2, "CustomerID", { nullable: true, references: "dbo.Customers.CustomerID" }),
    col(3, "Order Date", { dataType: "datetime2(0)" }),
  ],
  "dbo.Customers": [col(1, "CustomerID", { primaryKey: true }), col(2, "Name", { dataType: "nvarchar(100)" })],
  "sales.Orders": [col(1, "SalesOrderID")],
};

function source(overrides: Partial<MetadataSource> = {}): MetadataSource {
  return {
    objects: (db) => (db === "Sales" ? objects : db === "Archive" ? [{ schema: "dbo", name: "OldOrders", kind: ObjectKind.KindTable }] : undefined),
    columns: (_db, schema, name) => columns[`${schema}.${name}`],
    loadObjects: vi.fn(async () => {}),
    loadColumns: vi.fn(async () => {}),
    ...overrides,
  };
}

async function suggest(sql: string, src: MetadataSource | null = source(), budgetMs = 20) {
  const cursor = sql.indexOf("|");
  const text = sql.replace("|", "");
  return computeSuggestions({ textBefore: text.slice(0, cursor), statement: text, database: "Sales", source: src, budgetMs });
}

const labels = (r: { items: { label: string }[] }) => r.items.map((i) => i.label);

describe("computeSuggestions", () => {
  it("lists the alias's columns after alias. (the fixture acceptance case)", async () => {
    const r = await suggest("SELECT o.| FROM dbo.Orders AS o");
    expect(labels(r)).toEqual(["OrderID", "CustomerID", "Order Date"]);
    expect(r.items[2]).toMatchObject({ insertText: "[Order Date]", detail: "datetime2(0)" });
    expect(r.items[0]!.detail).toBe("int · PK");
    expect(r.items[1]!.detail).toBe("int · FK → dbo.Customers.CustomerID");
  });

  it("resolves unaliased tables, schemas and other databases", async () => {
    expect(labels(await suggest("SELECT Customers.| FROM Customers"))).toEqual(["CustomerID", "Name"]);
    expect(labels(await suggest("SELECT x.| FROM sales.Orders x"))).toEqual(["SalesOrderID"]);
    expect(labels(await suggest("SELECT * FROM sales.|"))).toEqual(["Orders"]);
    expect(labels(await suggest("SELECT dbo.Customers.|"))).toEqual(["CustomerID", "Name"]);
    expect(labels(await suggest("SELECT * FROM Archive.dbo.|"))).toEqual(["OldOrders"]);
  });

  it("suggests schema-qualified tables, views and functions after FROM/JOIN, quoted when needed", async () => {
    const r = await suggest("SELECT * FROM dbo.Orders o JOIN |");
    expect(labels(r)).toContain("dbo.Customers");
    expect(labels(r)).toContain("dbo.vActiveCustomers");
    expect(labels(r)).not.toContain("dbo.usp_Report");
    expect(r.items.find((i) => i.label === "dbo.Order Details")?.insertText).toBe("dbo.[Order Details]");
    expect(r.items.find((i) => i.label === "dbo.Orders")!.sortText < r.items.find((i) => i.label === "sales.Orders")!.sortText).toBe(true);
  });

  it("offers columns of referenced tables, aliases, keywords and functions elsewhere", async () => {
    const r = await suggest("SELECT | FROM dbo.Orders o JOIN dbo.Customers c ON c.CustomerID = o.CustomerID");
    expect(labels(r)).toEqual(expect.arrayContaining(["o", "c", "OrderID", "Name", "SELECT", "COUNT"]));
    expect(r.items.filter((i) => i.label === "CustomerID").map((i) => i.detail)).toEqual([
      "o · int · FK → dbo.Customers.CustomerID",
      "c · int · PK",
    ]);
    expect(r.items.find((i) => i.label === "COUNT")).toMatchObject({ insertText: "COUNT($0)", snippet: true });
  });

  it("offers nothing inside strings and comments, and only keywords without a connection", async () => {
    expect((await suggest("SELECT 'o.|' FROM dbo.Orders o")).items).toEqual([]);
    expect((await suggest("-- FROM |")).items).toEqual([]);
    const offline = await suggest("SELECT |", null);
    expect(labels(offline)).toContain("SELECT");
    expect(offline.items.every((i) => i.kind === "keyword" || i.kind === "builtin")).toBe(true);
    expect((await suggest("SELECT * FROM |", null)).items).toEqual([]);
  });

  it("loads missing metadata, but returns without it when the server is slow", async () => {
    let loaded = false;
    const lazy = source({
      objects: (db) => (db === "Sales" && loaded ? objects : undefined),
      loadObjects: vi.fn(async () => {
        loaded = true;
      }),
    });
    expect(labels(await suggest("SELECT * FROM |", lazy))).toContain("dbo.Orders");
    expect(lazy.loadObjects).toHaveBeenCalledWith("Sales");

    const slow = source({ objects: () => undefined, loadObjects: () => new Promise(() => {}) });
    const started = Date.now();
    const r = await suggest("SELECT * FROM |", slow, 30);
    expect(Date.now() - started).toBeLessThan(500);
    expect(r).toEqual({ items: [], incomplete: true });
  });

  describe("with the parser's scope", () => {
    const scope = (s: Partial<Scope>): Scope => ({ found: true, sources: [], ctes: [], ...s });
    const src = (s: Partial<ScopeSource> & Pick<ScopeSource, "name" | "kind">): ScopeSource => ({ depth: 0, ...s });
    async function suggestIn(sql: string, sc: Scope, metadata: MetadataSource | null = source()) {
      const cursor = sql.indexOf("|");
      const text = sql.replace("|", "");
      return computeSuggestions({
        textBefore: text.slice(0, cursor),
        statement: text,
        database: metadata ? "Sales" : null,
        source: metadata,
        budgetMs: 20,
        scope: sc,
      });
    }

    it("completes a CTE's columns, even before FROM uses it", async () => {
      const sc = scope({ ctes: [src({ name: "t", kind: "cte", columns: ["a", "bee"] })] });
      const r = await suggestIn("WITH t AS (SELECT a, b AS bee FROM dbo.X) SELECT t.|", sc);
      expect(labels(r)).toEqual(["a", "bee"]);
      expect(r.items[0]!.detail).toBe("cte");
      // Without a connection too: the query itself names the columns.
      expect(labels(await suggestIn("WITH t AS (SELECT a, b AS bee) SELECT t.|", sc, null))).toEqual(["a", "bee"]);
    });

    it("expands SELECT * in a CTE with the table's columns from metadata", async () => {
      const sc = scope({ ctes: [src({ name: "t", kind: "cte", tables: [{ schema: "dbo", name: "Customers" }] })] });
      expect(labels(await suggestIn("WITH t AS (SELECT * FROM dbo.Customers) SELECT t.|", sc))).toEqual(["CustomerID", "Name"]);
    });

    it("completes derived tables, and lists every visible source elsewhere", async () => {
      const derived = src({ name: "d", kind: "derived", columns: ["OrderID", "Customer"] });
      expect(labels(await suggestIn("SELECT d.| FROM (SELECT ...) AS d", scope({ sources: [derived] })))).toEqual(["OrderID", "Customer"]);

      const r = await suggestIn(
        "SELECT | FROM (SELECT ...) AS d JOIN dbo.Customers c ON ...",
        scope({ sources: [derived, src({ name: "c", kind: "table", object: { schema: "dbo", name: "Customers" } })] }),
      );
      expect(r.items.find((i) => i.label === "d")?.detail).toBe("derived table");
      expect(r.items.find((i) => i.label === "c")?.detail).toBe("alias of dbo.Customers");
      expect(r.items.filter((i) => i.kind === "column").map((i) => i.detail)).toEqual([
        "d · derived",
        "d · derived",
        "c · int · PK",
        "c · nvarchar(100)",
      ]);
    });

    it("ranks the inner query's sources before the outer ones in nested subqueries", async () => {
      const r = await suggestIn(
        "SELECT * FROM dbo.Orders o WHERE EXISTS (SELECT 1 FROM dbo.Customers c WHERE c.CustomerID = |)",
        scope({
          sources: [
            src({ name: "c", kind: "table", object: { schema: "dbo", name: "Customers" } }),
            src({ name: "o", kind: "table", object: { schema: "dbo", name: "Orders" }, depth: 1 }),
          ],
        }),
      );
      const sort = (label: string, owner: string) => r.items.find((i) => i.label === label && i.detail.startsWith(owner))!.sortText;
      expect(sort("Name", "c") < sort("OrderID", "o")).toBe(true);
    });

    it("offers the statement's CTEs where a table goes", async () => {
      const r = await suggestIn("WITH recent AS (SELECT 1 AS a) SELECT * FROM |", scope({ ctes: [src({ name: "recent", kind: "cte", columns: ["a"] })] }));
      expect(labels(r)[0]).toBe("recent");
      expect(labels(r)).toContain("dbo.Orders");
    });

    it("falls back to the heuristic for names the model does not know", async () => {
      // The text after the cursor did not parse, so the scope came from the text before it only.
      expect(labels(await suggestIn("SELECT o.| FROM dbo.Orders o WHERE", scope({})))).toEqual(["OrderID", "CustomerID", "Order Date"]);
      // A schema is not a source.
      expect(labels(await suggestIn("SELECT * FROM sales.|", scope({})))).toEqual(["Orders"]);
    });
  });

  it("formats column details like the explorer", () => {
    expect(columnDetail(col(1, "x", { dataType: "bigint", primaryKey: true, references: "dbo.T.id" }))).toBe("bigint · PK · FK → dbo.T.id");
  });
});
