import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useImperativeHandle } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { TooltipProvider } from "@/components/ui/tooltip";
import { Encoding } from "@/services/backend";
import { useEditorStore } from "@/stores/editor-store";
import { testConfig } from "@/test/fixtures";
import { EditorArea } from "./EditorArea";
import type { SqlEditorProps } from "./SqlEditor";

const api = vi.hoisted(() => ({
  openFile: vi.fn(),
  saveFile: vi.fn(),
  saveFileAs: vi.fn(),
  setUnsavedChanges: vi.fn(() => Promise.resolve()),
  closeWindow: vi.fn(() => Promise.resolve()),
  closeHandler: null as null | (() => void),
}));

vi.mock("@/services/backend", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/services/backend")>();
  return {
    ...original,
    backend: {
      ...original.backend,
      openFile: api.openFile,
      saveFile: api.saveFile,
      saveFileAs: api.saveFileAs,
      setUnsavedChanges: api.setUnsavedChanges,
      closeWindow: api.closeWindow,
      onCloseRequested: (handler: () => void) => {
        api.closeHandler = handler;
        return () => {};
      },
    },
  };
});

// Monaco does not run in jsdom; a textarea stands in for it, with the same props and handle.
vi.mock("./SqlEditor", async () => {
  const { formatSql } = await import("./format");
  return {
    default: function MockEditor(props: SqlEditorProps) {
      useImperativeHandle(props.handleRef, () => ({
        format: () => {
          const res = formatSql(props.value, props.formatting);
          if (res.ok) props.onChange(props.tabId, res.text);
          props.onFormatResult(res.ok ? null : res.error);
        },
        focus: () => {},
      }));
      return (
        <textarea
          aria-label="SQL editor"
          data-font-size={props.fontSize}
          data-theme={props.theme}
          value={props.value}
          onChange={(e) => props.onChange(props.tabId, e.target.value)}
        />
      );
    },
  };
});

function renderArea(binding = { connectionId: "c1", database: "Sales" }) {
  return render(
    <TooltipProvider>
      <EditorArea
        config={testConfig()}
        binding={binding}
        connectionName={(id) => (id === "c1" ? "Sales reporting" : null)}
        onCommand={vi.fn()}
        renderWelcome={(actions) => (
          <div>
            <p>Welcome</p>
            <button onClick={actions.newQuery}>Start a query</button>
            <button onClick={actions.openFile}>Open a file</button>
          </div>
        )}
      />
    </TooltipProvider>,
  );
}

const editor = () => screen.findByRole("textbox", { name: "SQL editor" });

beforeEach(() => {
  useEditorStore.setState({ tabs: [], activeId: null, untitledCount: 0 });
  vi.clearAllMocks();
  api.closeHandler = null;
});

describe("EditorArea", () => {
  it("shows the welcome page until a tab is opened, then an editor bound to the connection", async () => {
    renderArea();
    expect(screen.getByText("Welcome")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Start a query" }));

    expect(screen.getByRole("tab", { name: /Query 1/ })).toHaveAttribute("aria-selected", "true");
    expect(await editor()).toHaveAttribute("data-font-size", "14");
    expect(screen.getByLabelText("Tab connection")).toHaveTextContent("Sales reporting · Sales");
  });

  it("opens new tabs with Ctrl+N and closes clean ones with Ctrl+W", async () => {
    renderArea();
    fireEvent.keyDown(window, { key: "n", ctrlKey: true });
    fireEvent.keyDown(window, { key: "n", ctrlKey: true });
    expect(screen.getAllByRole("tab")).toHaveLength(2);
    fireEvent.keyDown(window, { key: "w", ctrlKey: true });
    expect(screen.getAllByRole("tab")).toHaveLength(1);
    expect(screen.getByRole("tab", { name: /Query 1/ })).toHaveAttribute("aria-selected", "true");
  });

  it("marks unsaved tabs and tells the backend", async () => {
    renderArea();
    fireEvent.keyDown(window, { key: "n", ctrlKey: true });
    fireEvent.change(await editor(), { target: { value: "SELECT 1;" } });
    expect(within(screen.getByRole("tab")).getByLabelText("Unsaved changes")).toBeInTheDocument();
    await waitFor(() => expect(api.setUnsavedChanges).toHaveBeenLastCalledWith(true));
  });

  it("asks before closing a tab with unsaved changes", async () => {
    renderArea();
    fireEvent.keyDown(window, { key: "n", ctrlKey: true });
    fireEvent.change(await editor(), { target: { value: "SELECT 1;" } });

    await userEvent.click(screen.getByRole("button", { name: "Close Query 1" }));
    const dialog = await screen.findByRole("alertdialog");
    expect(dialog).toHaveTextContent("Save changes to Query 1?");
    await userEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    expect(screen.getAllByRole("tab")).toHaveLength(1);

    await userEvent.click(screen.getByRole("button", { name: "Close Query 1" }));
    await userEvent.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Don't save" }));
    expect(screen.queryByRole("tab")).not.toBeInTheDocument();
    expect(screen.getByText("Welcome")).toBeInTheDocument();
  });

  it("saves before closing when asked to", async () => {
    api.saveFileAs.mockResolvedValue({ cancelled: false, path: "C:\\q\\a.sql", name: "a.sql", encoding: Encoding.UTF8BOM, error: "" });
    renderArea();
    fireEvent.keyDown(window, { key: "n", ctrlKey: true });
    fireEvent.change(await editor(), { target: { value: "SELECT 1;" } });
    await userEvent.click(screen.getByRole("button", { name: "Close Query 1" }));
    await userEvent.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Save" }));
    await waitFor(() => expect(screen.queryByRole("tab")).not.toBeInTheDocument());
    expect(api.saveFileAs).toHaveBeenCalledWith("Query 1", "SELECT 1;", "");
  });

  it("renames a tab by double-clicking it", async () => {
    renderArea();
    fireEvent.keyDown(window, { key: "n", ctrlKey: true });
    await userEvent.dblClick(screen.getByRole("tab"));
    const input = screen.getByRole("textbox", { name: "Tab name" });
    await userEvent.clear(input);
    await userEvent.type(input, "Orders check{Enter}");
    expect(screen.getByRole("tab", { name: /Orders check/ })).toBeInTheDocument();

    // Escape cancels.
    await userEvent.dblClick(screen.getByRole("tab"));
    await userEvent.type(screen.getByRole("textbox", { name: "Tab name" }), "zzz{Escape}");
    expect(screen.getByRole("tab", { name: /Orders check/ })).toBeInTheDocument();
  });

  it("formats the document with the formatting settings from the toolbar", async () => {
    renderArea();
    fireEvent.keyDown(window, { key: "n", ctrlKey: true });
    fireEvent.change(await editor(), { target: { value: "select a from t" } });
    await userEvent.click(screen.getByRole("button", { name: "Format document" }));
    expect(await editor()).toHaveValue("SELECT\n    a\nFROM\n    t");
  });

  it("shows formatting errors without changing the text", async () => {
    renderArea();
    fireEvent.keyDown(window, { key: "n", ctrlKey: true });
    fireEvent.change(await editor(), { target: { value: "select 'oops" } });
    await userEvent.click(screen.getByRole("button", { name: "Format document" }));
    expect(screen.getByRole("alert")).toHaveTextContent("Could not format");
    expect(await editor()).toHaveValue("select 'oops");
  });

  it("saves an untitled tab with Save As, then saves to the same file", async () => {
    api.saveFileAs.mockResolvedValue({ cancelled: false, path: "C:\\q\\report.sql", name: "report.sql", encoding: Encoding.UTF8BOM, error: "" });
    api.saveFile.mockResolvedValue({ cancelled: false, path: "C:\\q\\report.sql", name: "report.sql", encoding: Encoding.UTF8BOM, error: "" });
    renderArea();
    fireEvent.keyDown(window, { key: "n", ctrlKey: true });
    fireEvent.change(await editor(), { target: { value: "SELECT 1;" } });

    fireEvent.keyDown(window, { key: "s", ctrlKey: true });
    await waitFor(() => expect(screen.getByRole("tab", { name: /report\.sql/ })).toBeInTheDocument());
    expect(screen.queryByLabelText("Unsaved changes")).not.toBeInTheDocument();

    fireEvent.change(await editor(), { target: { value: "SELECT 2;" } });
    fireEvent.keyDown(window, { key: "s", ctrlKey: true });
    await waitFor(() => expect(api.saveFile).toHaveBeenCalledWith("C:\\q\\report.sql", "SELECT 2;", Encoding.UTF8BOM));
    expect(api.saveFileAs).toHaveBeenCalledTimes(1);

    // Ctrl+Shift+S always asks for a new name.
    fireEvent.keyDown(window, { key: "S", ctrlKey: true, shiftKey: true });
    await waitFor(() => expect(api.saveFileAs).toHaveBeenCalledTimes(2));
    expect(api.saveFileAs).toHaveBeenLastCalledWith("report.sql", "SELECT 2;", Encoding.UTF8BOM);
  });

  it("shows save errors and keeps the tab dirty", async () => {
    api.saveFileAs.mockResolvedValue({ cancelled: false, path: "", name: "", encoding: "", error: "You do not have permission to write a.sql." });
    renderArea();
    fireEvent.keyDown(window, { key: "n", ctrlKey: true });
    fireEvent.change(await editor(), { target: { value: "SELECT 1;" } });
    fireEvent.keyDown(window, { key: "s", ctrlKey: true });
    expect(await screen.findByRole("alert")).toHaveTextContent("permission");
    expect(screen.getByLabelText("Unsaved changes")).toBeInTheDocument();
  });

  it("opens a file with Ctrl+O", async () => {
    api.openFile.mockResolvedValue({
      cancelled: false,
      error: "",
      document: { path: "C:\\q\\orders.sql", name: "orders.sql", content: "SELECT * FROM dbo.Orders;", encoding: Encoding.UTF16LE },
    });
    renderArea();
    fireEvent.keyDown(window, { key: "o", ctrlKey: true });
    expect(await screen.findByRole("tab", { name: /orders\.sql/ })).toBeInTheDocument();
    expect(await editor()).toHaveValue("SELECT * FROM dbo.Orders;");
  });

  it("asks before the window closes with unsaved changes", async () => {
    renderArea();
    fireEvent.keyDown(window, { key: "n", ctrlKey: true });
    fireEvent.change(await editor(), { target: { value: "SELECT 1;" } });
    act(() => api.closeHandler?.());
    const dialog = await screen.findByRole("alertdialog");
    expect(dialog).toHaveTextContent("Query 1 has unsaved changes.");
    await userEvent.click(within(dialog).getByRole("button", { name: "Close without saving" }));
    expect(api.closeWindow).toHaveBeenCalled();
  });
});
