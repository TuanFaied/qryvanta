import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { testConfig, testInfo } from "@/test/fixtures";
import { SettingsDialog } from "./SettingsDialog";

function setup(onSave = vi.fn()) {
  const onOpenChange = vi.fn();
  render(
    <SettingsDialog
      open
      onOpenChange={onOpenChange}
      config={testConfig()}
      info={testInfo()}
      onSave={onSave}
      onLoadDefaults={vi.fn().mockResolvedValue(testConfig())}
      onOpenLogFolder={vi.fn().mockResolvedValue(undefined)}
    />,
  );
  return { onSave, onOpenChange, user: userEvent.setup() };
}

describe("SettingsDialog", () => {
  it("saves edited values and closes", async () => {
    const onSave = vi.fn().mockImplementation(async (config) => ({ ok: true, config, fieldErrors: {} }));
    const { user, onOpenChange } = setup(onSave);

    const maxRows = screen.getByLabelText("Maximum rows");
    await user.clear(maxRows);
    await user.type(maxRows, "500");
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(onSave).toHaveBeenCalledOnce();
    expect(onSave.mock.calls[0]![0].execution.maxRows).toBe(500);
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it("shows backend field errors and stays open", async () => {
    const onSave = vi.fn().mockImplementation(async (config) => ({
      ok: false,
      config,
      fieldErrors: { "execution.timeoutSeconds": "must be between 1 and 3600" },
    }));
    const { user, onOpenChange } = setup(onSave);

    const timeout = screen.getByLabelText("Query timeout");
    await user.clear(timeout);
    await user.type(timeout, "0");
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(await screen.findByText("Query timeout must be between 1 and 3600")).toBeInTheDocument();
    expect(onOpenChange).not.toHaveBeenCalledWith(false);
  });

  it("rejects non-numeric input without calling the backend", async () => {
    const { user, onSave } = setup();
    const timeout = screen.getByLabelText("Query timeout");
    await user.clear(timeout);
    await user.type(timeout, "abc");
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(screen.getByText("Query timeout must be a whole number")).toBeInTheDocument();
    expect(onSave).not.toHaveBeenCalled();
  });

  it("asks for confirmation before turning read-only mode off", async () => {
    const { user, onSave } = setup(vi.fn().mockImplementation(async (config) => ({ ok: true, config, fieldErrors: {} })));
    await user.click(screen.getByLabelText("Read-only mode"));
    expect(await screen.findByRole("alertdialog", { name: /Allow queries that modify data/ })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Keep read-only mode" }));
    expect(screen.getByLabelText("Read-only mode")).toBeChecked();
    expect(screen.queryByText(/modify data will be allowed/)).not.toBeInTheDocument();

    await user.click(screen.getByLabelText("Read-only mode"));
    await user.click(await screen.findByRole("button", { name: "Allow modifications" }));
    expect(screen.getByLabelText("Read-only mode")).not.toBeChecked();
    expect(screen.getByText(/modify data will be allowed/)).toBeInTheDocument();
    // The lock wait limit only applies in read-only mode, so it is hidden now.
    expect(screen.queryByLabelText("Lock wait limit (read-only mode)")).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Save" }));
    expect(onSave.mock.calls[0]![0].execution.readOnly).toBe(false);
  });

  it("turns read-only mode back on without confirmation and shows the lock wait limit", async () => {
    const { user } = setup();
    expect(screen.getByLabelText("Lock wait limit (read-only mode)")).toHaveValue("5");
    await user.click(screen.getByLabelText("Read-only mode"));
    await user.click(await screen.findByRole("button", { name: "Allow modifications" }));
    await user.click(screen.getByLabelText("Read-only mode"));
    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
    expect(screen.getByLabelText("Read-only mode")).toBeChecked();
  });
});
