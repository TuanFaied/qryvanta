import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { TooltipProvider } from "@/components/ui/tooltip";
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
    expect(screen.getByText("Read-only")).toBeInTheDocument();
    expect(screen.getByText(/Timeout 30s/)).toBeInTheDocument();
  });

  it("keeps execution disabled until a connection exists", () => {
    renderWorkbench();
    for (const name of ["Execute", "Execute with debugging", "Stop execution"]) {
      expect(screen.getByRole("button", { name })).toBeDisabled();
    }
  });

  it("shows startup warnings", () => {
    renderWorkbench({ ...testInfo(), startupWarnings: ["Config was reset"] });
    expect(screen.getByRole("alert")).toHaveTextContent("Config was reset");
  });
});
