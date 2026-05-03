import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
import path from "path";

// vitest.config.ts is intentionally separate from vite.config.ts.
// Reasons:
//   1. vite.config.ts pulls in @tailwindcss/vite which Vitest doesn't need
//      and which slows transform on every test run.
//   2. We don't need the dev server proxy block — tests don't make real HTTP.
// We DO need to mirror the path aliases from vite.config.ts so imports
// like @/components/foo and @beacon-shared/Bar resolve in tests too.
//
// If you add a new alias to vite.config.ts, add it here too.
export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
      "@beacon-shared": path.resolve(__dirname, "./src/shared"),
    },
  },
  test: {
    environment: "jsdom",
    globals: true, // describe/it/expect/vi available without import
    setupFiles: ["./test/setup.ts"],
    css: false, // no point processing tailwind for tests
    include: ["src/**/*.test.{ts,tsx}"],
  },
});
