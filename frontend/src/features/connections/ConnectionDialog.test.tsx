import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi, type Mock } from "vitest";
import { ErrorCode, PasswordAction, type ConnectionInput, type TestConnectionResult } from "@/services/backend";
import { testConnection, testServer } from "@/test/fixtures";
import { ConnectionDialog, type ConnectionDialogProps } from "./ConnectionDialog";

type SaveFn = ConnectionDialogProps["onSave"];
type TestFn = ConnectionDialogProps["onTest"];

function setup(opts: { editing?: ReturnType<typeof testConnection> | null; onSave?: Mock<SaveFn>; onTest?: Mock<TestFn> } = {}) {
  const onSave = opts.onSave ?? vi.fn<SaveFn>().mockResolvedValue({ ok: true, connection: testConnection(), fieldErrors: {} });
  const onTest = opts.onTest ?? vi.fn<TestFn>();
  const onOpenChange = vi.fn();
  render(<ConnectionDialog open editing={opts.editing ?? null} onOpenChange={onOpenChange} onSave={onSave} onTest={onTest} />);
  return { onSave, onTest, onOpenChange, user: userEvent.setup() };
}

const okResult = (): TestConnectionResult => ({ ok: true, server: testServer(), latencyMs: 12, access: null, error: null, fieldErrors: {} });

describe("ConnectionDialog", () => {
  it("creates a connection with a password", async () => {
    const { user, onSave, onOpenChange } = setup();
    await user.type(screen.getByLabelText("Name"), "Local");
    await user.type(screen.getByLabelText("Server"), "localhost");
    await user.type(screen.getByLabelText("User name"), "reader");
    await user.type(screen.getByLabelText("Password"), "pw!");
    await user.click(screen.getByRole("button", { name: "Save" }));

    const input = onSave.mock.calls[0]![0] as ConnectionInput;
    expect(input.profile).toMatchObject({ id: "", name: "Local", server: "localhost", username: "reader" });
    expect(input).toMatchObject({ passwordAction: PasswordAction.PasswordSet, password: "pw!" });
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it("shows backend field errors and stays open", async () => {
    const onSave = vi.fn<SaveFn>().mockResolvedValue({ ok: false, connection: null, fieldErrors: { server: "is required" } });
    const { user, onOpenChange } = setup({ onSave });
    await user.click(screen.getByRole("button", { name: "Save" }));
    expect(await screen.findByText("Server is required")).toBeInTheDocument();
    expect(onOpenChange).not.toHaveBeenCalledWith(false);
  });

  it("edits without revealing or resending the stored password", async () => {
    const { user, onSave } = setup({ editing: testConnection() });
    const password = screen.getByLabelText("Password");
    expect(password).toHaveValue("");
    expect(password).toHaveAttribute("placeholder", expect.stringMatching(/Saved/));
    await user.click(screen.getByRole("button", { name: "Save" }));
    expect(onSave.mock.calls[0]![0]).toMatchObject({ passwordAction: PasswordAction.PasswordKeep, password: "" });
  });

  it("can remove the saved password", async () => {
    const { user, onSave } = setup({ editing: testConnection() });
    await user.click(screen.getByLabelText("Remove saved password"));
    expect(screen.getByLabelText("Password")).toBeDisabled();
    await user.click(screen.getByRole("button", { name: "Save" }));
    expect(onSave.mock.calls[0]![0].passwordAction).toBe(PasswordAction.PasswordClear);
  });

  it("hides credentials for Windows authentication", async () => {
    const { user } = setup({ editing: testConnection({ auth: "windows" as never, username: "" }, { hasPassword: false }) });
    expect(screen.queryByLabelText("Password")).not.toBeInTheDocument();
    expect(screen.getByText(/connects as the Windows user/)).toBeInTheDocument();
    void user;
  });

  it("shows a successful test result", async () => {
    const onTest = vi.fn<TestFn>().mockResolvedValue(okResult());
    const { user } = setup({ editing: testConnection(), onTest });
    await user.click(screen.getByRole("button", { name: /Test connection/ }));
    const status = await screen.findByRole("status");
    expect(status).toHaveTextContent("Connected in 12 ms");
    expect(status).toHaveTextContent("16.0.4305.5 RTM");
    expect(onTest.mock.calls[0]![0].passwordAction).toBe(PasswordAction.PasswordKeep);
  });

  it("explains a certificate failure and clears the result after an edit", async () => {
    const onTest = vi.fn<TestFn>().mockResolvedValue({
      ok: false,
      server: testServer(),
      latencyMs: 0,
      access: null,
      error: { code: ErrorCode.ErrCertificate, serverErrorNumber: 0, message: "x509: certificate signed by unknown authority" },
      fieldErrors: {},
    });
    const { user } = setup({ editing: testConnection(), onTest });
    await user.click(screen.getByRole("button", { name: /Test connection/ }));
    expect(await screen.findByRole("alert")).toHaveTextContent(/Trust server certificate/);

    await user.type(screen.getByLabelText("Database"), "x");
    await waitFor(() => expect(screen.queryByText("Connection failed")).not.toBeInTheDocument());
  });

  it("cancels a running test", async () => {
    let signal: AbortSignal | undefined;
    const onTest = vi.fn<TestFn>().mockImplementation((_input, s) => {
      signal = s;
      return new Promise(() => {});
    });
    const { user } = setup({ editing: testConnection(), onTest });
    await user.click(screen.getByRole("button", { name: /Test connection/ }));
    await user.click(await screen.findByRole("button", { name: "Cancel test" }));
    expect(signal?.aborted).toBe(true);
    expect(await screen.findByRole("button", { name: /Test connection/ })).toBeEnabled();
  });
});

describe("ConnectionDialog write access", () => {
  it("shows the login's write access in a successful test", async () => {
    const onTest = vi.fn<TestFn>().mockResolvedValue({
      ...okResult(),
      access: { database: "Sales", level: "can_write" as never, reasons: [{ kind: "direct", text: "member of the db_owner database role" }], error: "" },
    });
    const { user } = setup({ editing: testConnection(), onTest });
    await user.click(screen.getByRole("button", { name: /Test connection/ }));
    const status = await screen.findByRole("status");
    expect(status).toHaveTextContent("Login can modify data");
    expect(status).toHaveTextContent("member of the db_owner database role");
  });
});
