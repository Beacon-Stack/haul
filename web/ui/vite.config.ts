import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import path from "path";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
      "@beacon-shared": path.resolve(__dirname, "../../../web-shared"),
      // Force React singleton. See pilot/web/ui/vite.config.ts for context.
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
