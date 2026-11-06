/**
 * Helpers for the Content-Security-Policy <meta> tag written into index.html by production
 * builds. The policy text lives in frontend/csp-production.txt and is shared with the Go
 * asset server, which sends it as a header.
 */

// Directives that browsers ignore (and warn about) when the policy is delivered by <meta>.
const headerOnlyDirectives = ["frame-ancestors", "report-uri", "sandbox"];

/** Removes directives that are only valid in the Content-Security-Policy header. */
export function metaPolicy(policy: string): string {
  return policy
    .split(";")
    .map((d) => d.trim())
    .filter((d) => d && !headerOnlyDirectives.includes(d.split(/\s+/)[0]!.toLowerCase()))
    .join("; ");
}

/** Inserts the CSP <meta> tag right after <meta charset>, so it applies before any script. */
export function injectCspMeta(html: string, policy: string): string {
  const content = metaPolicy(policy).replaceAll("&", "&amp;").replaceAll('"', "&quot;");
  const tag = `<meta http-equiv="Content-Security-Policy" content="${content}" />`;
  const charset = /<meta\s+charset=[^>]*>/i;
  if (charset.test(html)) return html.replace(charset, (m) => `${m}\n    ${tag}`);
  return html.replace(/<head[^>]*>/i, (m) => `${m}\n    ${tag}`);
}
