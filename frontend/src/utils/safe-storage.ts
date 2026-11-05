/**
 * localStorage wrapper that never throws. Used only for per-user UI conveniences such as
 * panel sizes. Anything that must persist reliably goes to the Go backend instead.
 */
export const safeStorage = {
  getItem(key: string): string | null {
    try {
      return window.localStorage.getItem(key);
    } catch {
      return null;
    }
  },
  setItem(key: string, value: string): void {
    try {
      window.localStorage.setItem(key, value);
    } catch {
      // Storage unavailable or full: the UI falls back to defaults next time.
    }
  },
};
