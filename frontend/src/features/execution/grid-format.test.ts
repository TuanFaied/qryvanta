import { describe, expect, it } from "vitest";
import type { ResultColumn } from "@/services/backend";
import { describeColumn, formatValue, initialWidth, isNumericColumn, rangeToTsv, toTsv } from "./grid-format";

const cols: ResultColumn[] = [
  { name: "id", type: "INT", nullable: false },
  { name: "name", type: "NVARCHAR", nullable: true },
  { name: "active", type: "BIT", nullable: true },
];

describe("grid formatting", () => {
  it("formats NULL, bit and other values as SQL Server tools do", () => {
    expect(formatValue(null)).toBe("NULL");
    expect(formatValue(true)).toBe("1");
    expect(formatValue(false)).toBe("0");
    expect(formatValue(42)).toBe("42");
    expect(formatValue("12345678901234567890.1234")).toBe("12345678901234567890.1234");
  });

  it("describes columns for the header tooltip and aligns numbers right", () => {
    expect(describeColumn(cols[1]!)).toBe("name: NVARCHAR, NULL");
    expect(describeColumn(cols[0]!)).toBe("id: INT, NOT NULL");
    expect(describeColumn({ name: "", type: "INT", nullable: false })).toBe("(no column name): INT, NOT NULL");
    expect(isNumericColumn(cols[0]!)).toBe(true);
    expect(isNumericColumn(cols[1]!)).toBe(false);
  });

  it("copies as TSV with CRLF lines, NULL as NULL, and quoting for tabs, newlines and quotes", () => {
    const rows = [
      [1, "Ada", true],
      [2, null, false],
      [3, 'say "hi"\tthere\nnext', null],
    ];
    expect(toTsv(cols, rows, [0, 1, 2], { headers: true })).toBe(
      ["id\tname\tactive", "1\tAda\t1", "2\tNULL\t0", '3\t"say ""hi""\tthere\nnext"\tNULL'].join("\r\n"),
    );
  });

  it("copies a selected rectangle in either drag direction", () => {
    const rows = [
      [1, "a", true],
      [2, "b", false],
      [3, "c", true],
    ];
    const range = { anchor: { row: 2, col: 1 }, focus: { row: 1, col: 0 } };
    expect(rangeToTsv(cols, rows, range)).toBe("2\tb\r\n3\tc");
    expect(rangeToTsv(cols, rows, range, { headers: true })).toBe("id\tname\r\n2\tb\r\n3\tc");
    expect(rangeToTsv(cols, rows, { anchor: { row: 0, col: 2 }, focus: { row: 0, col: 2 } })).toBe("1");
  });

  it("sizes columns from the header and the first rows, within bounds", () => {
    expect(initialWidth(cols[0]!, [], 0)).toBe(64);
    expect(initialWidth(cols[1]!, [[1, "x".repeat(20), true]], 1)).toBe(20 * 7 + 20);
    expect(initialWidth(cols[1]!, [[1, "x".repeat(500), true]], 1)).toBe(420);
  });
});
