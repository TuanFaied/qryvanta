import { afterEach, describe, expect, it, vi } from "vitest";
import { backend, type Scope } from "@/services/backend";
import { scopeFor } from "./scope";

const found: Scope = { found: true, sources: [{ name: "o", kind: "table", depth: 0 }], ctes: [] };

afterEach(() => vi.restoreAllMocks());

describe("scopeFor", () => {
  it("returns the parser's scope for the whole text and the cursor offset", async () => {
    const spy = vi.spyOn(backend, "completionScope").mockResolvedValue(found);
    const signal = new AbortController().signal;
    expect(await scopeFor("SELECT o. FROM dbo.Orders o", 9, signal)).toBe(found);
    expect(spy).toHaveBeenCalledWith("SELECT o. FROM dbo.Orders o", 9, signal);
  });

  it("falls back (null) when the text does not parse or the backend fails", async () => {
    vi.spyOn(backend, "completionScope").mockResolvedValueOnce({ found: false, sources: [], ctes: [] });
    expect(await scopeFor("SELEC", 5, new AbortController().signal)).toBeNull();
    vi.spyOn(backend, "completionScope").mockRejectedValueOnce(new Error("parser unavailable"));
    expect(await scopeFor("SELECT", 6, new AbortController().signal)).toBeNull();
  });

  it("never waits longer than the completion budget", async () => {
    vi.spyOn(backend, "completionScope").mockReturnValue(new Promise(() => {}));
    const started = Date.now();
    expect(await scopeFor("SELECT", 6, new AbortController().signal, 30)).toBeNull();
    expect(Date.now() - started).toBeLessThan(400);
  });
});
