import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { testConfig } from "@/test/fixtures";
import { StatusBar } from "./StatusBar";

const problem = (line: number) => ({ number: 102, message: "Incorrect syntax near 'FROM'.", offset: 0, byteOffset: 0, line, column: 8 });

describe("StatusBar problems", () => {
  it("counts the tab's syntax errors and goes to the first one on click", async () => {
    const onShowProblems = vi.fn();
    render(
      <StatusBar
        config={testConfig()}
        onOpenSettings={vi.fn()}
        connection={null}
        problems={[problem(3), problem(7)]}
        onShowProblems={onShowProblems}
      />,
    );
    const chip = screen.getByRole("button", { name: /2 problems/ });
    expect(chip).toHaveAttribute("title", expect.stringContaining("Line 3, column 8: Incorrect syntax near 'FROM'."));
    await userEvent.click(chip);
    expect(onShowProblems).toHaveBeenCalled();
  });

  it("shows nothing without problems", () => {
    render(<StatusBar config={testConfig()} onOpenSettings={vi.fn()} connection={null} />);
    expect(screen.queryByRole("button", { name: /problem/ })).not.toBeInTheDocument();
  });
});
