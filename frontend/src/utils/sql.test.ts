import { describe, expect, it } from "vitest";
import { qualifiedName, quoteIdent, selectTop100 } from "./sql";

describe("sql helpers", () => {
  it("quotes identifiers like QUOTENAME", () => {
    expect(quoteIdent("Orders")).toBe("[Orders]");
    expect(quoteIdent("Order Items")).toBe("[Order Items]");
    expect(quoteIdent("a]b")).toBe("[a]]b]");
    expect(qualifiedName("dbo", "x]; DROP")).toBe("[dbo].[x]]; DROP]");
  });

  it("builds SELECT TOP (100) with explicit columns when known", () => {
    expect(selectTop100("dbo", "Orders", ["OrderID", "Status"])).toBe(
      "SELECT TOP (100)\n    [OrderID],\n    [Status]\nFROM [dbo].[Orders];",
    );
    expect(selectTop100("sales", "vw Totals")).toBe("SELECT TOP (100)\n    *\nFROM [sales].[vw Totals];");
  });
});
