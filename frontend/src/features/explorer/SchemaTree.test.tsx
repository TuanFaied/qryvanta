import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const api = vi.hoisted(() => ({
  listDatabases: vi.fn(),
  listObjects: vi.fn(),
  listColumns: vi.fn(),
}));

vi.mock("@/services/backend", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/services/backend")>();
  return { ...actual, backend: { ...actual.backend, ...api } };
});

const { useSchemaStore } = await import("@/stores/schema-store");
const { SchemaTree } = await import("./SchemaTree");

function renderTree(filter = "") {
  return render(
    <ul role="tree">
      <SchemaTree connectionId="c1" filter={filter} />
    </ul>,
  );
}

describe("SchemaTree", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useSchemaStore.setState({ databases: {}, objects: {}, columns: {}, expanded: {}, currentDatabase: {} });
    api.listDatabases.mockResolvedValue({
      data: [
        { name: "QryvantaSample", state: "ONLINE", system: false, readOnly: false, accessible: true },
        { name: "Archive", state: "ONLINE", system: false, readOnly: false, accessible: false },
        { name: "master", state: "ONLINE", system: true, readOnly: false, accessible: true },
      ],
      error: null,
    });
    api.listObjects.mockResolvedValue({
      data: [
        { schema: "dbo", name: "Customers", kind: "table" },
        { schema: "dbo", name: "Orders", kind: "table" },
        { schema: "dbo", name: "vw_OrderTotals", kind: "view" },
        { schema: "dbo", name: "usp_CustomerOrders", kind: "procedure" },
      ],
      error: null,
    });
    api.listColumns.mockResolvedValue({
      data: [
        { ordinal: 1, name: "OrderID", dataType: "int", nullable: false, identity: false, computed: false, primaryKey: true, references: "" },
        { ordinal: 2, name: "CustomerID", dataType: "int", nullable: true, identity: false, computed: false, primaryKey: false, references: "dbo.Customers.CustomerID" },
      ],
      error: null,
    });
  });

  it("lazy-loads databases, objects and columns", async () => {
    const user = userEvent.setup();
    renderTree();
    const db = await screen.findByRole("treeitem", { name: "QryvantaSample" });
    expect(api.listObjects).not.toHaveBeenCalled();

    await user.click(db);
    expect(api.listObjects).toHaveBeenCalledWith("c1", "QryvantaSample");
    await user.click(await screen.findByRole("treeitem", { name: "Tables" }));
    await user.click(await screen.findByRole("treeitem", { name: "dbo.Orders" }));
    expect(api.listColumns).toHaveBeenCalledWith("c1", "QryvantaSample", "dbo", "Orders");

    const pk = await screen.findByRole("treeitem", { name: "OrderID" });
    expect(pk).toHaveTextContent("int, NOT NULL");
    expect(within(pk).getByLabelText("primary key")).toBeInTheDocument();
    const fk = screen.getByRole("treeitem", { name: "CustomerID" });
    expect(fk).toHaveAttribute("title", expect.stringContaining("References dbo.Customers.CustomerID"));
    expect(fk).toHaveTextContent("int, NULL");
  });

  it("keeps system databases in a folder and marks inaccessible ones", async () => {
    const user = userEvent.setup();
    renderTree();
    const archive = await screen.findByRole("treeitem", { name: "Archive" });
    expect(archive).toHaveTextContent("no access");
    expect(archive).not.toHaveAttribute("aria-expanded");
    expect(screen.queryByRole("treeitem", { name: "master" })).not.toBeInTheDocument();
    await user.click(screen.getByRole("treeitem", { name: "System databases" }));
    expect(screen.getByRole("treeitem", { name: "master" })).toBeInTheDocument();
  });

  it("makes a database current on double-click", async () => {
    const user = userEvent.setup();
    renderTree();
    await user.dblClick(await screen.findByRole("treeitem", { name: "QryvantaSample" }));
    expect(useSchemaStore.getState().currentDatabase.c1).toBe("QryvantaSample");
  });

  it("filters objects and opens matching folders", async () => {
    const user = userEvent.setup();
    const { rerender } = renderTree();
    await user.click(await screen.findByRole("treeitem", { name: "QryvantaSample" }));
    await screen.findByRole("treeitem", { name: "Tables" });

    rerender(
      <ul role="tree">
        <SchemaTree connectionId="c1" filter="order" />
      </ul>,
    );
    expect(screen.getByRole("treeitem", { name: "dbo.Orders" })).toBeInTheDocument();
    expect(screen.getByRole("treeitem", { name: "dbo.vw_OrderTotals" })).toBeInTheDocument();
    expect(screen.getByRole("treeitem", { name: "dbo.usp_CustomerOrders" })).toBeInTheDocument();
    expect(screen.queryByRole("treeitem", { name: "dbo.Customers" })).not.toBeInTheDocument();
    expect(screen.queryByRole("treeitem", { name: "Functions" })).not.toBeInTheDocument();
  });

  it("shows node errors with a retry", async () => {
    const user = userEvent.setup();
    api.listObjects.mockResolvedValueOnce({
      data: [],
      error: { code: "permission", serverErrorNumber: 916, message: "The server principal is not able to access the database" },
    });
    renderTree();
    await user.click(await screen.findByRole("treeitem", { name: "QryvantaSample" }));
    expect(await screen.findByText(/not able to access/)).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Retry" }));
    await waitFor(() => expect(screen.getByRole("treeitem", { name: "Tables" })).toBeInTheDocument());
  });

  it("supports keyboard expansion", async () => {
    const user = userEvent.setup();
    renderTree();
    const db = await screen.findByRole("treeitem", { name: "QryvantaSample" });
    db.focus();
    await user.keyboard("{ArrowRight}");
    expect(db).toHaveAttribute("aria-expanded", "true");
    await user.keyboard("{ArrowLeft}");
    expect(db).toHaveAttribute("aria-expanded", "false");
  });
});
