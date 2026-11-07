import { describe, expect, it } from "vitest";
import { detectContext, findTableRefs, quoteIfNeeded, statementAround, tokenize } from "./context";

describe("detectContext (heuristic)", () => {
  it.each([
    ["SELECT * FROM ", "object"],
    ["SELECT * FROM Ord", "object"],
    ["SELECT * FROM [Order Det", "object"],
    ["select * from dbo.Orders o inner join ", "object"],
    ["SELECT * FROM a LEFT OUTER JOIN Cust", "object"],
    ["SELECT * FROM a, ", "object"],
    ["SELECT * FROM a AS x, b y, ", "object"],
    ["UPDATE ", "object"],
    ["INSERT INTO ", "object"],
    ["DELETE FROM ", "object"],
    ["SELECT * FROM t CROSS APPLY ", "object"],
    ["SELECT ", "general"],
    ["SELECT a, ", "general"],
    ["SELECT * FROM t WHERE ", "general"],
    ["SELECT * FROM t WHERE x IN (1, ", "general"],
    ["SELECT * FROM t ORDER BY a, ", "general"],
    ["SELECT * FROM (SELECT a, ", "general"],
  ])("%j → %s", (text, kind) => {
    expect(detectContext(text).kind).toBe(kind);
  });

  it("returns the qualifier after a dot, ignoring the partial word", () => {
    expect(detectContext("SELECT o.")).toEqual({ kind: "member", qualifier: ["o"] });
    expect(detectContext("SELECT o.Cust")).toEqual({ kind: "member", qualifier: ["o"] });
    expect(detectContext("SELECT * FROM dbo.")).toEqual({ kind: "member", qualifier: ["dbo"] });
    expect(detectContext("SELECT * FROM Sales.dbo.Or")).toEqual({ kind: "member", qualifier: ["Sales", "dbo"] });
    expect(detectContext("SELECT [Order Lines].")).toEqual({ kind: "member", qualifier: ["Order Lines"] });
  });

  it("offers nothing inside strings and comments", () => {
    for (const text of ["SELECT 'FROM ", "SELECT N'it''s FROM ", "-- FROM ", "/* SELECT o.", "SELECT 1 /* x */ -- o."]) {
      expect(detectContext(text)).toEqual({ kind: "none" });
    }
    // Closed strings and comments do not hide what follows.
    expect(detectContext("SELECT 'a' AS x /* c */ FROM ").kind).toBe("object");
    expect(detectContext("-- comment\nSELECT * FROM ").kind).toBe("object");
  });
});

describe("findTableRefs (heuristic)", () => {
  it("finds tables with schema, database and alias", () => {
    expect(
      findTableRefs("SELECT o. FROM dbo.Orders AS o JOIN Sales.dbo.Customers c ON c.id = o.cid LEFT JOIN Products WHERE 1=1"),
    ).toEqual([
      { database: null, schema: "dbo", name: "Orders", alias: "o" },
      { database: "Sales", schema: "dbo", name: "Customers", alias: "c" },
      { database: null, schema: null, name: "Products", alias: null },
    ]);
  });

  it("handles comma joins, bracketed names, table hints and DML targets", () => {
    expect(findTableRefs("SELECT * FROM [Order Details] od WITH (NOLOCK), dbo.Products AS p")).toEqual([
      { database: null, schema: null, name: "Order Details", alias: "od" },
      { database: null, schema: "dbo", name: "Products", alias: "p" },
    ]);
    expect(findTableRefs("UPDATE t SET x = 1").map((r) => r.name)).toEqual(["t"]);
    expect(findTableRefs("INSERT INTO dbo.Log (a) VALUES (1)").map((r) => r.name)).toEqual(["Log"]);
  });

  it("skips table-valued functions and variables", () => {
    expect(findTableRefs("SELECT * FROM dbo.fn_rows(1) AS f JOIN @t AS t ON 1=1")).toEqual([]);
  });

  it("does not take keywords for aliases", () => {
    expect(findTableRefs("SELECT * FROM Orders WHERE x = 1 ORDER BY x")[0]?.alias).toBeNull();
    expect(findTableRefs("SELECT * FROM Orders JOIN Customers ON 1=1").map((r) => r.alias)).toEqual([null, null]);
  });
});

describe("statementAround", () => {
  it("limits analysis to the statement at the cursor", () => {
    const text = "SELECT * FROM a AS x;\nSELECT x. FROM b AS x;\nSELECT 3";
    const offset = text.indexOf("x. ") + 2;
    const s = statementAround(text, offset);
    expect(s.text).toBe("\nSELECT x. FROM b AS x");
    const go = "SELECT 1\nGO\nSELECT * FROM t";
    expect(statementAround(go, go.length).text).toBe("\nSELECT * FROM t");
  });
});

describe("quoteIfNeeded", () => {
  it.each([
    ["Orders", "Orders"],
    ["order_lines2", "order_lines2"],
    ["Müller", "Müller"],
    ["Order Details", "[Order Details]"],
    ["2026Sales", "[2026Sales]"],
    ["Order", "[Order]"],
    ["user", "[user]"],
    ["a]b", "[a]]b]"],
    ["dash-name", "[dash-name]"],
  ])("%j → %j", (name, quoted) => {
    expect(quoteIfNeeded(name)).toBe(quoted);
  });
});

describe("tokenize", () => {
  it("reads bracketed and quoted identifiers with escapes", () => {
    const { tokens } = tokenize('SELECT [a]]b], "c d" FROM t');
    expect(tokens.filter((t) => t.kind === "word").map((t) => (t as { text: string }).text)).toEqual(["SELECT", "a]b", "c d", "FROM", "t"]);
  });
});
