import { describe, expect, it } from "vitest";
import { KeywordCase } from "@/services/backend";
import { formatSql } from "./format";

const upper = { keywordCase: KeywordCase.KeywordUpper, indentSize: 4, useTabs: false };

describe("formatSql", () => {
  it("formats T-SQL with upper-case keywords and the configured indent", () => {
    const res = formatSql("select top (10) o.id, c.name from dbo.Orders o join dbo.Customers c on c.id = o.customer_id", upper);
    expect(res).toEqual({
      ok: true,
      text: [
        "SELECT",
        "    TOP (10) o.id,",
        "    c.name",
        "FROM",
        "    dbo.Orders o",
        "    JOIN dbo.Customers c ON c.id = o.customer_id",
      ].join("\n"),
    });
  });

  it("supports lower case, preserved case and tabs", () => {
    const lower = formatSql("SELECT A FROM T", { ...upper, keywordCase: KeywordCase.KeywordLower, useTabs: true });
    expect(lower).toEqual({ ok: true, text: "select\n\tA\nfrom\n\tT" });
    const kept = formatSql("Select a From t", { ...upper, keywordCase: KeywordCase.KeywordPreserve, indentSize: 2 });
    expect(kept).toEqual({ ok: true, text: "Select\n  a\nFrom\n  t" });
  });

  it("understands T-SQL syntax: bracketed names, variables, N-strings and temp tables", () => {
    const res = formatSql("declare @d date = '2026-01-01'; select [Order ID], N'x' from #tmp where d >= @d", upper);
    expect(res.ok).toBe(true);
    if (res.ok) {
      expect(res.text).toContain("DECLARE @d DATE = '2026-01-01';");
      expect(res.text).toContain("[Order ID]");
      expect(res.text).toContain("#tmp");
      expect(res.text).toContain("d >= @d");
    }
  });

  it("keeps a trailing newline and leaves blank input alone", () => {
    expect(formatSql("select 1\n", upper)).toEqual({ ok: true, text: "SELECT\n    1\n" });
    expect(formatSql("  \n", upper)).toEqual({ ok: true, text: "  \n" });
  });

  it("reports text it cannot tokenize instead of changing it", () => {
    const res = formatSql("select 'unterminated", upper);
    expect(res.ok).toBe(false);
    if (!res.ok) expect(res.error).toMatch(/^Could not format:/);
  });
});
