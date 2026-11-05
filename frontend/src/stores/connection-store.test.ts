import { beforeEach, describe, expect, it, vi } from "vitest";
import { ErrorCode } from "@/services/backend";
import { testConnection, testServer } from "@/test/fixtures";

const api = vi.hoisted(() => ({
  listConnections: vi.fn(),
  saveConnection: vi.fn(),
  deleteConnection: vi.fn(),
  connect: vi.fn(),
  disconnect: vi.fn(),
}));

vi.mock("@/services/backend", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/services/backend")>();
  return { ...actual, backend: { ...actual.backend, ...api } };
});

const { useConnectionStore } = await import("./connection-store");

describe("connection store", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useConnectionStore.setState({ connections: [], activeId: null, activeServer: null, connectError: null, connectingId: null });
    api.listConnections.mockResolvedValue([testConnection(), testConnection({ id: "c2", name: "Other" })]);
    api.disconnect.mockResolvedValue(undefined);
  });

  it("connects and keeps one active connection", async () => {
    api.connect.mockResolvedValue({ ok: true, server: testServer(), error: null });
    api.listConnections.mockResolvedValue([testConnection({}, { connected: true }), testConnection({ id: "c2", name: "Other" })]);
    await useConnectionStore.getState().connect("c1");
    expect(useConnectionStore.getState().activeId).toBe("c1");

    api.listConnections.mockResolvedValue([testConnection({}, { connected: true }), testConnection({ id: "c2", name: "Other" }, { connected: true })]);
    await useConnectionStore.getState().connect("c2");
    expect(api.disconnect).toHaveBeenCalledWith("c1");
    expect(useConnectionStore.getState().activeId).toBe("c2");
    expect(useConnectionStore.getState().activeServer?.productVersion).toBe("16.0.4305.5");
  });

  it("records connect failures without changing the active connection", async () => {
    api.connect.mockResolvedValue({
      ok: false,
      server: testServer(),
      error: { code: ErrorCode.ErrLoginFailed, serverErrorNumber: 18456, message: "Login failed" },
    });
    const ok = await useConnectionStore.getState().connect("c1");
    const s = useConnectionStore.getState();
    expect(ok).toBe(false);
    expect(s.activeId).toBeNull();
    expect(s.connectError).toMatchObject({ id: "c1", error: { serverErrorNumber: 18456 } });
    expect(s.connectingId).toBeNull();
  });

  it("clears the active connection when it is deleted", async () => {
    useConnectionStore.setState({ activeId: "c1", activeServer: testServer() });
    api.deleteConnection.mockResolvedValue(undefined);
    api.listConnections.mockResolvedValue([testConnection({ id: "c2", name: "Other" })]);
    await useConnectionStore.getState().remove("c1");
    expect(useConnectionStore.getState().activeId).toBeNull();
    expect(useConnectionStore.getState().connections).toHaveLength(1);
  });

  it("drops the active connection after saving it, because the backend closes the handle", async () => {
    useConnectionStore.setState({ activeId: "c1", activeServer: testServer() });
    api.saveConnection.mockResolvedValue({ ok: true, connection: testConnection(), fieldErrors: {} });
    await useConnectionStore.getState().save({ profile: testConnection().profile, passwordAction: "keep" as never, password: "" });
    expect(useConnectionStore.getState().activeId).toBeNull();
  });
});
