import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import path from "path";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
      // Shared React components are vendored from beacon-stack/web-shared
      // via `git subtree add --prefix=web/ui/src/shared` and sync upstream
      // bugfixes with `git subtree pull`.
      "@beacon-shared": path.resolve(__dirname, "./src/shared"),
      // Force React singleton. Kept as free insurance even after vendoring.
      react: path.resolve(__dirname, "node_modules/react"),
      "react-dom": path.resolve(__dirname, "node_modules/react-dom"),
    },
    dedupe: ["react", "react-dom"],
  },
  build: {
    outDir: "../static",
    emptyOutDir: true,
  },
  server: {
    proxy: {
      "/api": {
        target: "http://localhost:8484",
        changeOrigin: true,
        ws: true,
      },
      "/health": {
        target: "http://localhost:8484",
        changeOrigin: true,
      },
    },
  },
});
