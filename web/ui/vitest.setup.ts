import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";
import { afterEach } from "vitest";

// jsdom doesn't ship ResizeObserver. The torrent detail page's PieceBar
// canvas widget relies on it for responsive resizing — without this
// polyfill, every test that renders TorrentDetail crashes inside
// useEffect with `ReferenceError: ResizeObserver is not defined`.
// A no-op stub is sufficient because tests don't simulate viewport
// changes.
class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}
if (typeof globalThis.ResizeObserver === "undefined") {
  globalThis.ResizeObserver = ResizeObserverStub as unknown as typeof ResizeObserver;
}

afterEach(() => {
  cleanup();
});
