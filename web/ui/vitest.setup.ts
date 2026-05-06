// jsdom doesn't ship ResizeObserver. PieceBar's useEffect uses
// `new ResizeObserver(...)` at component-render time, so without a
// polyfill every test that renders TorrentDetail crashes with
// `ReferenceError: ResizeObserver is not defined`.
//
// Define this BEFORE importing @testing-library/jest-dom — that import
// can pull in DOM globals that close over the resolution path.
class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}
(globalThis as unknown as { ResizeObserver: typeof ResizeObserver }).ResizeObserver =
  ResizeObserverStub as unknown as typeof ResizeObserver;
if (typeof window !== "undefined") {
  (window as unknown as { ResizeObserver: typeof ResizeObserver }).ResizeObserver =
    ResizeObserverStub as unknown as typeof ResizeObserver;
}

import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";
import { afterEach } from "vitest";

afterEach(() => {
  cleanup();
});
