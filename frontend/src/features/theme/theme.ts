import { useEffect, useState } from "react";
import { Theme } from "@/services/backend";

export type ResolvedTheme = "dark" | "light";

const darkQuery = "(prefers-color-scheme: dark)";

/** Resolves the configured theme to the theme that is actually shown. */
export function resolveTheme(theme: Theme, prefersDark: boolean): ResolvedTheme {
  switch (theme) {
    case Theme.ThemeLight:
      return "light";
    case Theme.ThemeSystem:
      return prefersDark ? "dark" : "light";
    default:
      // Dark is the default, including for unknown or empty values.
      return "dark";
  }
}

export function systemPrefersDark(): boolean {
  return typeof window !== "undefined" && typeof window.matchMedia === "function"
    ? window.matchMedia(darkQuery).matches
    : true;
}

/** Applies the theme to the document root. */
export function applyTheme(theme: Theme, root: HTMLElement = document.documentElement): ResolvedTheme {
  const resolved = resolveTheme(theme, systemPrefersDark());
  root.classList.toggle("dark", resolved === "dark");
  root.style.colorScheme = resolved;
  return resolved;
}

/** The theme actually shown, following the OS while the setting is "system". */
export function useResolvedTheme(theme: Theme): ResolvedTheme {
  const [prefersDark, setPrefersDark] = useState(systemPrefersDark);
  useEffect(() => {
    if (theme !== Theme.ThemeSystem) return;
    setPrefersDark(systemPrefersDark());
    return watchSystemTheme(() => setPrefersDark(systemPrefersDark()));
  }, [theme]);
  return resolveTheme(theme, prefersDark);
}

/** Calls onChange when the OS theme changes. Returns an unsubscribe function. */
export function watchSystemTheme(onChange: () => void): () => void {
  if (typeof window === "undefined" || typeof window.matchMedia !== "function") return () => {};
  const mql = window.matchMedia(darkQuery);
  mql.addEventListener("change", onChange);
  return () => mql.removeEventListener("change", onChange);
}
