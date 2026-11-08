import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { TooltipProvider } from "@/components/ui/tooltip";
import { backend } from "@/services/backend";
import { testConfig, testInfo } from "@/test/fixtures";
import { Workbench } from "./Workbench";

function renderWorkbench(info = testInfo()) {
  return render(
    <TooltipProvider>
      <Workbench
        info={info}
        config={testConfig()}
        onThemeChange={vi.fn()}
        onSaveConfig={vi.fn()}
        onLoadDefaults={vi.fn()}
        onOpenLogFolder={vi.fn()}
      />
    </TooltipProvider>,
  );
}

describe("Workbench", () => {
  it("renders every shell region", () => {
    renderWorkbench();
    expect(screen.getByRole("region", { name: "Database explorer" })).toBeInTheDocument();
    expect(screen.getByRole("main", { name: "Workspace" })).toBeInTheDocument();
    expect(screen.getByRole("region", { name: "Output" })).toBeInTheDocument();
    expect(screen.getByRole("region", { name: "Analysis" })).toBeInTheDocument();
  });

  it("shows the backend version and execution policy", () => {
    renderWorkbench();
    expect(screen.getByText("v0.1.0-test")).toBeInTheDocument();
    expect(screen.getByText("Read-only mode")).toBeInTheDocument();
    expect(screen.getByText(/Timeout 30s/)).toBeInTheDocument();
  });

  it("keeps execution disabled until a connection exists", () => {
    renderWorkbench();
    for (const name of ["Execute", "Execute with debugging", "Stop execution"]) {
      expect(screen.getByRole("button", { name })).toBeDisabled();
    }
  });

  it("warns when the T-SQL parser is unavailable, with the reason", async () => {
    const spy = vi.spyOn(backend, "parserStatus").mockResolvedValue({
      available: false,
      reason: "the parser was not found at C:\\app\\qryvanta-tsqlparser.exe",
      version: "",
      scriptDom: "",
      protocol: 0,
      path: "C:\\app\\qryvanta-tsqlparser.exe",
    });
    renderWorkbench();
    const chip = await screen.findByRole("button", { name: /Parser unavailable/ });
    expect(chip).toHaveAttribute("title", expect.stringContaining("was not found"));
    spy.mockResolvedValue({ available: true, reason: "", version: "0.1.0", scriptDom: "18.0", protocol: 1, path: "" });
    await userEvent.click(chip);
    await waitFor(() => expect(screen.queryByRole("button", { name: /Parser unavailable/ })).not.toBeInTheDocument());
    spy.mockRestore();
  });

  it("shows nothing about the parser when it works", async () => {
    const spy = vi.spyOn(backend, "parserStatus").mockResolvedValue({
      available: true, reason: "", version: "0.1.0", scriptDom: "18.0", protocol: 1, path: "",
    });
    renderWorkbench();
    await waitFor(() => expect(spy).toHaveBeenCalled());
    expect(screen.queryByText(/Parser unavailable/)).not.toBeInTheDocument();
    spy.mockRestore();
  });

  it("shows startup warnings", () => {
    renderWorkbench({ ...testInfo(), startupWarnings: ["Config was reset"] });
    expect(screen.getByRole("alert")).toHaveTextContent("Config was reset");
  });
});
