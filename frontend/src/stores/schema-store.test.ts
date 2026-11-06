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

const { dbKey, useSchemaStore } = await import("./schema-store");

describe("schema store", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useSchemaStore.setState({ databases: {}, objects: {}, columns: {}, expanded: {}, currentDatabase: {} });
    api.listDatabases.mockResolvedValue({ data: [{ name: "Sales", state: "ONLINE", system: false, readOnly: false, accessible: true }], error: null });
    api.listObjects.mockResolvedValue({ data: [{ schema: "dbo", name: "Orders", kind: "table" }], error: null });
  });

  it("loads each node once unless forced", async () => {
    const s = useSchemaStore.getState();
    await s.loadDatabases("c1");
    await s.loadDatabases("c1");
    expect(api.listDatabases).toHaveBeenCalledTimes(1);
    await s.loadDatabases("c1", true);
    expect(api.listDatabases).toHaveBeenCalledTimes(2);
    expect(useSchemaStore.getState().databases.c1?.status).toBe("ready");
  });

  it("keeps the last good data when a refresh fails, and retries after an error", async () => {
    const s = useSchemaStore.getState();
    await s.loadObjects("c1", "Sales");
    api.listObjects.mockResolvedValueOnce({ data: [], error: { code: "timeout", serverErrorNumber: 0, message: "slow" } });
    await s.loadObjects("c1", "Sales", true);
    const node = useSchemaStore.getState().objects[dbKey("c1", "Sales")];
    expect(node?.status).toBe("error");
    expect(node?.data).toHaveLength(1);

    await s.loadObjects("c1", "Sales"); // not forced, but errors are retried
    expect(api.listObjects).toHaveBeenCalledTimes(3);
  });

  it("turns a rejected call into an error node", async () => {
    api.listDatabases.mockRejectedValueOnce(new Error("Load databases: backend gone"));
    await useSchemaStore.getState().loadDatabases("c1");
    expect(useSchemaStore.getState().databases.c1).toMatchObject({ status: "error", error: { message: "Load databases: backend gone" } });
  });

  it("forgets only the given connection", async () => {
    const s = useSchemaStore.getState();
    await s.loadDatabases("c1");
    await s.loadDatabases("c2");
    await s.loadObjects("c1", "Sales");
    s.setExpanded(dbKey("c1", "Sales"), true);
    s.setExpanded(dbKey("c2", "Sales"), true);
    s.forget("c1");
    const st = useSchemaStore.getState();
    expect(st.databases.c1).toBeUndefined();
    expect(st.objects[dbKey("c1", "Sales")]).toBeUndefined();
    expect(st.expanded[dbKey("c1", "Sales")]).toBeUndefined();
    expect(st.databases.c2).toBeDefined();
    expect(st.expanded[dbKey("c2", "Sales")]).toBe(true);
  });
});
