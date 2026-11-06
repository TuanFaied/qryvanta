import { beforeEach, describe, expect, it, vi } from "vitest";

const api = vi.hoisted(() => ({ checkWriteAccess: vi.fn() }));

vi.mock("@/services/backend", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/services/backend")>();
  return { ...actual, backend: { ...actual.backend, ...api } };
});

const { accessKey, useAccessStore } = await import("./access-store");

describe("access store", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useAccessStore.setState({ results: {}, loading: {} });
    api.checkWriteAccess.mockResolvedValue({ database: "Sales", level: "read_only", reasons: [], error: "" });
  });

  it("checks once per connection and database unless forced", async () => {
    const s = useAccessStore.getState();
    await s.check("c1", "Sales");
    await s.check("c1", "Sales");
    await s.check("c1", "master");
    expect(api.checkWriteAccess).toHaveBeenCalledTimes(2);
    await s.check("c1", "Sales", true);
    expect(api.checkWriteAccess).toHaveBeenCalledTimes(3);
    expect(useAccessStore.getState().results[accessKey("c1", "Sales")]?.level).toBe("read_only");
  });

  it("records a failed call as unknown", async () => {
    api.checkWriteAccess.mockRejectedValueOnce(new Error("Check write access: backend gone"));
    await useAccessStore.getState().check("c1", "Sales");
    expect(useAccessStore.getState().results[accessKey("c1", "Sales")]).toMatchObject({ level: "unknown", error: "Check write access: backend gone" });
  });

  it("forgets one connection", async () => {
    const s = useAccessStore.getState();
    await s.check("c1", "Sales");
    await s.check("c2", "Sales");
    s.forget("c1");
    const results = useAccessStore.getState().results;
    expect(results[accessKey("c1", "Sales")]).toBeUndefined();
    expect(results[accessKey("c2", "Sales")]).toBeDefined();
  });
});
