import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { injectCspMeta, metaPolicy } from "./csp";

// Vitest runs from frontend/; import.meta.url is not a file: URL under jsdom.
const policy = readFileSync(resolve(process.cwd(), "csp-production.txt"), "utf8").trim();

describe("production Content-Security-Policy", () => {
  it("forbids inline scripts, eval and external sources", () => {
    const script = policy.split(";").map((d) => d.trim()).find((d) => d.startsWith("script-src"));
    expect(script).toBe("script-src 'self'");
    expect(policy).not.toContain("unsafe-eval");
    expect(policy).toContain("object-src 'none'");
    expect(policy).toContain("connect-src 'self'");
    expect(policy).not.toMatch(/https?:\/\//);
  });
});

describe("metaPolicy", () => {
  it("drops directives that are only valid in the header", () => {
    expect(metaPolicy("default-src 'self'; frame-ancestors 'none'; sandbox; object-src 'none'")).toBe(
      "default-src 'self'; object-src 'none'",
    );
  });
});

describe("injectCspMeta", () => {
  it("places the tag right after the charset meta, before any script", () => {
    const html = `<!doctype html><html><head>\n<meta charset="UTF-8" />\n<script type="module" src="/a.js"></script></head></html>`;
    const out = injectCspMeta(html, policy);
    const meta = out.indexOf('http-equiv="Content-Security-Policy"');
    expect(meta).toBeGreaterThan(out.indexOf("charset"));
    expect(meta).toBeLessThan(out.indexOf("<script"));
    expect(out).not.toContain("frame-ancestors");
  });

  it("falls back to the start of <head> and escapes quotes", () => {
    const out = injectCspMeta("<html><head><title>x</title></head></html>", `default-src "self"`);
    expect(out).toContain('<head>\n    <meta http-equiv="Content-Security-Policy" content="default-src &quot;self&quot;" />');
  });
});
