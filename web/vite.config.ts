/// <reference types="vitest/config" />
import { writeFileSync } from "node:fs";
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

export default defineConfig({
  plugins: [react(), tailwindcss(), keepGitkeep()],
  resolve: {
    alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) },
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
