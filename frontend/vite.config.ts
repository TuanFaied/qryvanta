/// <reference types="vitest/config" />
import { readFileSync, writeFileSync } from "node:fs";
import { fileURLToPath, URL } from "node:url";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import wails from "@wailsio/runtime/plugins/vite";
import { defineConfig, type Plugin } from "vite";
import { injectCspMeta } from "./src/lib/csp";

// Go embeds frontend/dist with //go:embed, which fails when the directory is empty.
// The repository keeps dist/.gitkeep so a fresh clone compiles; Vite empties dist on build,
// so this plugin puts the file back.
function keepDistPlaceholder(): Plugin {
  return {
    name: "qryvanta:keep-dist-placeholder",
    apply: "build",
    closeBundle() {
      writeFileSync(fileURLToPath(new URL("./dist/.gitkeep", import.meta.url)), "");
    },
  };
}

// Production builds carry the Content-Security-Policy in index.html as well as in the header
// the Go asset server sends, so the page is protected even if a webview ignored the header.
// Development builds (Vite dev server, `--mode development`) skip it: React Refresh needs an
// inline script there. The Go side applies its relaxed development policy instead.
function contentSecurityPolicy(): Plugin {
  let production = false;
  return {
    name: "qryvanta:content-security-policy",
    apply: "build",
    configResolved(config) {
      production = config.mode === "production";
    },
    transformIndexHtml(html) {
      if (!production) return html;
      const policy = readFileSync(fileURLToPath(new URL("./csp-production.txt", import.meta.url)), "utf8").trim();
      return injectCspMeta(html, policy);
    },
  };
}

export default defineConfig({
  server: {
    host: "127.0.0.1",
    port: Number(process.env.WAILS_VITE_PORT) || 9245,
    strictPort: true,
  },
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
      // Monaco's package exports map every path to a .js file, so its stylesheets cannot be
      // imported by package path. src/features/editor/monaco.ts imports them through this alias.
      "monaco-esm": fileURLToPath(new URL("./node_modules/monaco-editor/esm", import.meta.url)),
    },
  },
  build: {
    // Monaco is one large chunk (loaded lazily with the first editor tab).
    chunkSizeWarningLimit: 4096,
  },
  plugins: [react(), tailwindcss(), wails("./bindings"), contentSecurityPolicy(), keepDistPlaceholder()],
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["./src/test/setup.ts"],
    css: false,
  },
});
