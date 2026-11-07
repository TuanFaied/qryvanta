import { fireEvent, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterAll, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import type { ResultSetData } from "@/stores/execution-store";
import { ResultGrid } from "./ResultGrid";

const copied = vi.hoisted(() => ({ text: [] as string[] }));
vi.mock("@/utils/clipboard", () => ({
  copyText: async (t: string) => {
    copied.text.push(t);
    return true;
  },
}));

const resultSet: ResultSetData = {
  index: 0,
  columns: [
    { name: "id", type: "INT", nullable: false },
    { name: "city", type: "NVARCHAR", nullable: true },
  ],
  rows: [
    [1, "Colombo"],
    [2, null],
    [3, "Kandy"],
  ],
};

const cell = (row: number, col: number) =>
  within(screen.getAllByRole("row")[row + 1]!).getAllByRole("gridcell")[col]!;

beforeEach(() => (copied.text = []));

// jsdom has no layout; the virtualizer reads the scroll container's clientWidth/Height (0×0 there).
const original = {
  w: Object.getOwnPropertyDescriptor(Element.prototype, "clientWidth"),
  h: Object.getOwnPropertyDescriptor(Element.prototype, "clientHeight"),
};
beforeAll(() => {
  Object.defineProperty(Element.prototype, "clientWidth", { configurable: true, get: () => 1000 });
  Object.defineProperty(Element.prototype, "clientHeight", { configurable: true, get: () => 600 });
});
afterAll(() => {
  if (original.w) Object.defineProperty(Element.prototype, "clientWidth", original.w);
  if (original.h) Object.defineProperty(Element.prototype, "clientHeight", original.h);
});

describe("ResultGrid", () => {
  it("renders headers with their SQL type and shows NULL distinctly", () => {
    render(<ResultGrid resultSet={resultSet} label="Result 1" />);
    expect(screen.getByRole("columnheader", { name: /^city/ })).toHaveAttribute("title", "city: NVARCHAR, NULL");
    const nullCell = cell(1, 1);
    expect(nullCell).toHaveTextContent("NULL");
    expect(within(nullCell).getByText("NULL")).toHaveClass("italic");
    // A string that says "NULL" is not styled as NULL.
    expect(cell(0, 1)).toHaveTextContent("Colombo");
  });

  it("copies a cell, a shift-selected range, and everything", async () => {
    render(<ResultGrid resultSet={resultSet} label="Result 1" />);
    const grid = screen.getByRole("grid", { name: "Result 1" });

    fireEvent.mouseDown(cell(0, 1), { button: 0 });
    expect(cell(0, 1)).toHaveAttribute("aria-selected", "true");
    fireEvent.keyDown(grid, { key: "c", ctrlKey: true });

    fireEvent.mouseDown(cell(1, 1), { button: 0, shiftKey: true });
    fireEvent.keyDown(grid, { key: "c", ctrlKey: true });

    fireEvent.keyDown(grid, { key: "a", ctrlKey: true });
    fireEvent.keyDown(grid, { key: "c", ctrlKey: true, shiftKey: true });

    await vi.waitFor(() => expect(copied.text).toHaveLength(3));
    expect(copied.text).toEqual([
      "Colombo",
      "Colombo\r\nNULL",
      "id\tcity\r\n1\tColombo\r\n2\tNULL\r\n3\tKandy",
    ]);
  });

  it("selects whole rows from the row number and moves with the arrow keys", () => {
    render(<ResultGrid resultSet={resultSet} label="Result 1" />);
    const grid = screen.getByRole("grid", { name: "Result 1" });
    fireEvent.mouseDown(screen.getAllByRole("rowheader")[2]!, { button: 0 });
    expect(cell(2, 0)).toHaveAttribute("aria-selected", "true");
    expect(cell(2, 1)).toHaveAttribute("aria-selected", "true");

    fireEvent.mouseDown(cell(0, 0), { button: 0 });
    fireEvent.keyDown(grid, { key: "ArrowDown", shiftKey: true });
    expect(cell(1, 0)).toHaveAttribute("aria-selected", "true");
    expect(cell(1, 1)).toHaveAttribute("aria-selected", "false");
  });

  it("offers copy commands in the context menu", async () => {
    render(<ResultGrid resultSet={resultSet} label="Result 1" />);
    fireEvent.contextMenu(cell(2, 0));
    await userEvent.click(await screen.findByRole("menuitem", { name: "Copy all with headers" }));
    await vi.waitFor(() => expect(copied.text).toHaveLength(1));
    expect(copied.text[0]).toBe("id\tcity\r\n1\tColombo\r\n2\tNULL\r\n3\tKandy");
  });
});
