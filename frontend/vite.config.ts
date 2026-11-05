/// <reference types="vitest/config" />
import { writeFileSync } from "node:fs";
import { fileURLToPath, URL } from "node:url";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import wails from "@wailsio/runtime/plugins/vite";
import { defineConfig, type Plugin } from "vite";

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

export default defineConfig({
  server: {
    host: "127.0.0.1",
    port: Number(process.env.WAILS_VITE_PORT) || 9245,
    strictPort: true,
  },
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
    },
  },
  plugins: [react(), tailwindcss(), wails("./bindings"), keepDistPlaceholder()],
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["./src/test/setup.ts"],
    css: false,
  },
});
