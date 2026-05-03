// Vitest setup — runs once per test file before the tests start.
//
// What this does:
//   - Registers @testing-library/jest-dom matchers (toBeInTheDocument,
//     toHaveTextContent, toHaveAttribute, etc.) on Vitest's expect.
//   - Auto-cleans up rendered React trees between tests via afterEach.
//     RTL's cleanup() is safe to call even when nothing's mounted.
//
// If you need a per-test-file setup, add a `beforeAll` inside the test
// file itself — don't bloat this global hook.

import "@testing-library/jest-dom/vitest";
import { afterEach } from "vitest";
import { cleanup } from "@testing-library/react";

afterEach(() => {
  cleanup();
});
