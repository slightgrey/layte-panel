/// <reference types="vitest/config" />
import { realpathSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath, URL } from "node:url";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig, type Plugin } from "vite";

// Vite empties dist/ on every build; put back the .gitkeep that lets
// `go build` embed the folder before the UI has ever been built.
const keepGitkeep = (): Plugin => ({
  name: "keep-gitkeep",
  apply: "build",
  closeBundle() {
    writeFileSync(fileURLToPath(new URL("./dist/.gitkeep", import.meta.url)), "");
  },
});

// The project folder in its canonical spelling. On Windows the same folder can
// arrive as "d:\…" (npm sets the working directory that way) or "D:\…"
// (shells, and node_modules paths after symlink resolution). Vite treats the
// two spellings as different files, so React got bundled twice (blank page:
// "Cannot read properties of null (reading 'useContext')") and Vitest loaded
// itself twice ("failed to find the current suite"). realpathSync.native asks
// the OS for the one true spelling; root and alias both use it.
const here = realpathSync.native(fileURLToPath(new URL(".", import.meta.url)));

export default defineConfig({
  root: here,
  plugins: [react(), tailwindcss(), keepGitkeep()],
  resolve: {
    alias: { "@": join(here, "src") },
  },
  server: {
    port: 5173,
    // The Go server (`layte serve --dev`) handles the API; same-origin for the browser.
    proxy: { "/api": "http://127.0.0.1:8443" },
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
    sourcemap: false,
  },
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["./src/test/setup.ts"],
  },
});
