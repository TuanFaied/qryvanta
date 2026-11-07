import { beforeEach, describe, expect, it } from "vitest";
import { Encoding } from "@/services/backend";
import { isDirty, unsavedTabs, useEditorStore } from "./editor-store";

const s = () => useEditorStore.getState();
const doc = (path: string, content = "SELECT 1;") => ({ path, name: path.split("\\").pop()!, content, encoding: Encoding.UTF8BOM });
const noBinding = { connectionId: null, database: null };

beforeEach(() => useEditorStore.setState({ tabs: [], activeId: null, untitledCount: 0 }));

describe("editor store", () => {
  it("numbers new tabs and activates them, keeping their binding", () => {
    const a = s().newTab({ connectionId: "c1", database: "Sales" });
    const b = s().newTab();
    expect(s().tabs.map((t) => t.title)).toEqual(["Query 1", "Query 2"]);
    expect(s().activeId).toBe(b);
    expect(s().tabs.find((t) => t.id === a)).toMatchObject({ connectionId: "c1", database: "Sales" });
  });

  it("does not reuse numbers after a tab closes", () => {
    const a = s().newTab();
    s().closeTab(a);
    s().newTab();
    expect(s().tabs[0]?.title).toBe("Query 2");
  });

  it("activates the right neighbour on close, or the left one for the last tab", () => {
    const a = s().newTab();
    const b = s().newTab();
    const c = s().newTab();
    s().activate(b);
    s().closeTab(b);
    expect(s().activeId).toBe(c);
    s().closeTab(c);
    expect(s().activeId).toBe(a);
    s().closeTab(a);
    expect(s().activeId).toBeNull();
  });

  it("tracks unsaved changes against the saved content", () => {
    const id = s().newTab();
    expect(unsavedTabs(s().tabs)).toHaveLength(0);
    s().setContent(id, "SELECT 1;");
    expect(isDirty(s().tabs[0]!)).toBe(true);
    s().setContent(id, "");
    expect(isDirty(s().tabs[0]!)).toBe(false);
  });

  it("renames, ignoring blank names", () => {
    const id = s().newTab();
    s().rename(id, "  Orders check ");
    s().rename(id, "   ");
    expect(s().tabs[0]?.title).toBe("Orders check");
  });

  it("opens a file into an untouched empty tab, and activates a file that is already open", () => {
    const empty = s().newTab();
    const opened = s().openDocument(doc("C:\\q\\orders.sql"), noBinding);
    expect(opened).toBe(empty);
    expect(s().tabs).toHaveLength(1);
    expect(s().tabs[0]).toMatchObject({ title: "orders.sql", path: "C:\\q\\orders.sql", content: "SELECT 1;" });
    expect(isDirty(s().tabs[0]!)).toBe(false);

    const other = s().newTab();
    s().setContent(other, "x");
    expect(s().openDocument(doc("c:\\Q\\ORDERS.sql"), noBinding)).toBe(empty);
    expect(s().activeId).toBe(empty);

    s().activate(other);
    const second = s().openDocument(doc("C:\\q\\customers.sql"), { connectionId: "c1", database: "Sales" });
    expect(s().tabs).toHaveLength(3);
    expect(s().tabs.find((t) => t.id === second)).toMatchObject({ connectionId: "c1", database: "Sales" });
  });

  it("marks a tab saved and names it after a new file", () => {
    const id = s().newTab();
    s().setContent(id, "SELECT 2;");
    s().markSaved(id, { path: "C:\\q\\report.sql", name: "report.sql", encoding: Encoding.UTF8BOM, content: "SELECT 2;" });
    expect(s().tabs[0]).toMatchObject({ title: "report.sql", path: "C:\\q\\report.sql", encoding: Encoding.UTF8BOM });
    expect(isDirty(s().tabs[0]!)).toBe(false);

    // Saving the same file again keeps a custom tab name.
    s().rename(id, "Report");
    s().markSaved(id, { path: "C:\\q\\report.sql", name: "report.sql", encoding: Encoding.UTF8BOM, content: "SELECT 2;" });
    expect(s().tabs[0]?.title).toBe("Report");
  });

  it("keeps edits made during a save dirty", () => {
    const id = s().newTab();
    s().setContent(id, "SELECT 3;");
    s().setContent(id, "SELECT 3; -- edited while the dialog was open");
    s().markSaved(id, { path: "C:\\q\\a.sql", name: "a.sql", encoding: Encoding.UTF8BOM, content: "SELECT 3;" });
    expect(isDirty(s().tabs[0]!)).toBe(true);
  });
});
