import { describe, expect, it, vi } from "vitest";
import { ObjectKind, type Column, type SchemaObject } from "@/services/backend";
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

  it("formats column details like the explorer", () => {
    expect(columnDetail(col(1, "x", { dataType: "bigint", primaryKey: true, references: "dbo.T.id" }))).toBe("bigint · PK · FK → dbo.T.id");
  });
});
