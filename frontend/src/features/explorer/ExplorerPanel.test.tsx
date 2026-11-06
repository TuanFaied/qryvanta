import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { TooltipProvider } from "@/components/ui/tooltip";
import { testConnection } from "@/test/fixtures";
import { ExplorerPanel, type ExplorerPanelProps } from "./ExplorerPanel";

function setup(overrides: Partial<ExplorerPanelProps> = {}) {
  const props: ExplorerPanelProps = {
    connections: [
      testConnection(),
      testConnection({ id: "c2", name: "Local Express", server: String.raw`localhost\SQLEXPRESS`, database: "", auth: "windows" as never }),
    ],
    activeId: null,
    connectingId: null,
    loading: false,
    loadError: null,
    onRefresh: vi.fn(),
    onNew: vi.fn(),
    onEdit: vi.fn(),
    onDelete: vi.fn(),
    onConnect: vi.fn(),
    onDisconnect: vi.fn(),
    ...overrides,
  };
  render(
    <TooltipProvider>
      <ExplorerPanel {...props} />
    </TooltipProvider>,
  );
  return { props, user: userEvent.setup() };
}

describe("ExplorerPanel", () => {
  it("offers to create the first connection", async () => {
    const { props, user } = setup({ connections: [] });
    await user.click(screen.getAllByRole("button", { name: /New connection/ })[1]!);
    expect(props.onNew).toHaveBeenCalled();
  });

  it("lists connections and connects on double-click", async () => {
    const { props, user } = setup();
    expect(screen.getByRole("treeitem", { name: "Sales reporting" })).toHaveTextContent("db1.corp.local / Sales");
    await user.dblClick(screen.getByRole("treeitem", { name: "Local Express" }));
    expect(props.onConnect).toHaveBeenCalledWith("c2");
  });

  it("supports keyboard actions", () => {
    const { props } = setup();
    const row = screen.getByRole("treeitem", { name: "Sales reporting" });
    fireEvent.keyDown(row, { key: "F2" });
    fireEvent.keyDown(row, { key: "Delete" });
    fireEvent.keyDown(row, { key: "Enter" });
    expect(props.onEdit).toHaveBeenCalled();
    expect(props.onDelete).toHaveBeenCalled();
    expect(props.onConnect).toHaveBeenCalledWith("c1");
  });

  it("filters connections", async () => {
    const { user } = setup();
    await user.click(screen.getByRole("button", { name: "Search" }));
    await user.type(screen.getByLabelText("Filter connections and objects"), "express");
    expect(screen.queryByRole("treeitem", { name: "Sales reporting" })).not.toBeInTheDocument();
    expect(screen.getByRole("treeitem", { name: "Local Express" })).toBeInTheDocument();
  });

  it("marks the active connection", () => {
    setup({ activeId: "c1", connections: [testConnection({}, { connected: true })] });
    const row = screen.getByRole("treeitem", { name: "Sales reporting" });
    expect(row).toHaveAttribute("aria-selected", "true");
    expect(row).toHaveTextContent("connected");
  });

  it("shows load errors with a retry", async () => {
    const { props, user } = setup({ loadError: "Load connections: disk error" });
    expect(screen.getByText("Load connections: disk error")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Try again" }));
    expect(props.onRefresh).toHaveBeenCalled();
  });
});
