import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { AccessIndicator } from "@/components/shell/StatusBar";
import { AccessLevel, type WriteAccess } from "@/services/backend";
import { describeAccess } from "./access";

const access = (level: AccessLevel, reasons: string[] = []): WriteAccess => ({
  database: "Sales",
  level,
  reasons: reasons.map((text) => ({ kind: level === AccessLevel.AccessMayWrite ? "indirect" : "direct", text })),
  error: level === AccessLevel.AccessUnknown ? "permission check timed out" : "",
});

describe("describeAccess", () => {
  it("labels each level", () => {
    expect(describeAccess(undefined)).toMatchObject({ tone: "muted", label: "Checking permissions…" });
    expect(describeAccess(access(AccessLevel.AccessReadOnly))).toMatchObject({ tone: "ok", label: "Read-only login" });
    expect(describeAccess(access(AccessLevel.AccessMayWrite))).toMatchObject({ tone: "warn" });
    expect(describeAccess(access(AccessLevel.AccessCanWrite))).toMatchObject({ tone: "danger", label: "Login can modify data" });
    expect(describeAccess(access(AccessLevel.AccessUnknown)).detail).toContain("permission check timed out");
  });
});

describe("AccessIndicator", () => {
  beforeEach(() => vi.clearAllMocks());

  it("lists the reasons and rechecks on click", async () => {
    const onRecheck = vi.fn();
    render(<AccessIndicator access={access(AccessLevel.AccessCanWrite, ["member of the sysadmin server role"])} onRecheck={onRecheck} />);
    const button = screen.getByRole("button", { name: /Login can modify data/ });
    expect(button).toHaveAttribute("title", expect.stringContaining("member of the sysadmin server role"));
    await userEvent.setup().click(button);
    expect(onRecheck).toHaveBeenCalled();
  });

  it("tolerates a null reasons list from the bindings", () => {
    render(<AccessIndicator access={{ ...access(AccessLevel.AccessReadOnly), reasons: null }} />);
    expect(screen.getByRole("button", { name: /Read-only login/ })).toBeInTheDocument();
  });
});
