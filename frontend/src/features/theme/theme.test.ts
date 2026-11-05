import { describe, expect, it } from "vitest";
import { Theme } from "@/services/backend";
import { applyTheme, resolveTheme } from "./theme";

describe("resolveTheme", () => {
  it("resolves explicit themes", () => {
    expect(resolveTheme(Theme.ThemeDark, false)).toBe("dark");
    expect(resolveTheme(Theme.ThemeLight, true)).toBe("light");
  });

  it("follows the OS for system", () => {
    expect(resolveTheme(Theme.ThemeSystem, true)).toBe("dark");
    expect(resolveTheme(Theme.ThemeSystem, false)).toBe("light");
  });

  it("defaults to dark for unknown values", () => {
    expect(resolveTheme(Theme.$zero, false)).toBe("dark");
  });
});

describe("applyTheme", () => {
  it("toggles the dark class and color-scheme", () => {
    const root = document.createElement("html");
    applyTheme(Theme.ThemeLight, root);
    expect(root.classList.contains("dark")).toBe(false);
    expect(root.style.colorScheme).toBe("light");

    applyTheme(Theme.ThemeDark, root);
    expect(root.classList.contains("dark")).toBe(true);
    expect(root.style.colorScheme).toBe("dark");
  });
});
