// TorrentDetail tests focus on a single load-bearing concern: the stall
// callout banner is shown ONLY when /api/v1/torrents/{hash}/stall returns
// stalled=true. Two cases:
//
//   1. callout RENDERS when the stall hook returns a classification.
//   2. callout is ABSENT when the stall hook returns stalled=false.
//
// We mock @/api/torrents so we don't go near the real network or the
// detail page's other hooks (peers/pieces/trackers). The shared confirm
// dialog provider isn't needed because the stall callout never opens it.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Routes, Route } from "react-router-dom";
import { ConfirmProvider } from "@beacon-shared/ConfirmDialog";
import type { TorrentInfo, StallInfo, PiecesInfo, TrackerInfo, PeerInfo, TorrentFile } from "@/api/torrents";

// Hoisted mock state — vi.mock factories are hoisted above imports, so
// stash the per-test return values on the global vi handle and update
// them in beforeEach before each render.
const stallState: { value: StallInfo | undefined } = { value: undefined };

vi.mock("@/api/torrents", async () => {
  const actual = await vi.importActual<typeof import("@/api/torrents")>("@/api/torrents");
  // useTorrent / useTorrentFiles / etc. — plain shims that match the
  // real { data, isLoading } shape react-query exposes. The component
  // doesn't read isPending / status / error fields, so this is sufficient.
  const torrent: TorrentInfo = {
    info_hash: "deadbeef",
    name: "test-torrent",
    status: "downloading",
    save_path: "/tmp",
    category: "",
    tags: null,
    size: 1000,
    downloaded: 100,
    uploaded: 0,
    progress: 0.1,
    download_rate: 0,
    upload_rate: 0,
    seeds: 0,
    peers: 0,
    seed_ratio: 0,
    eta: 0,
    added_at: new Date().toISOString(),
    content_path: "/tmp/test-torrent",
    sequential: false,
    stalled: true,
  };
  const pieces: PiecesInfo = { num_pieces: 0, piece_size: 0, runs: [] };
  const peers: PeerInfo[] = [];
  const trackers: TrackerInfo[] = [];
  const files: TorrentFile[] = [];

  return {
    ...actual,
    useTorrent: () => ({ data: torrent, isLoading: false }),
    useTorrentFiles: () => ({ data: files }),
    useTorrentPeers: () => ({ data: { peers } }),
    useTorrentPieces: () => ({ data: pieces }),
    useTorrentTrackers: () => ({ data: { trackers } }),
    useTorrentStall: () => ({ data: stallState.value }),
    usePauseTorrent: () => ({ mutate: vi.fn() }),
    useResumeTorrent: () => ({ mutate: vi.fn() }),
    useDeleteTorrent: () => ({ mutate: vi.fn() }),
  };
});

// Imported AFTER the vi.mock above so the mocked module wins.
import TorrentDetail from "./TorrentDetail";

function renderDetail() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <ConfirmProvider>
        <MemoryRouter initialEntries={["/torrents/deadbeef"]}>
          <Routes>
            <Route path="/torrents/:hash" element={<TorrentDetail />} />
          </Routes>
        </MemoryRouter>
      </ConfirmProvider>
    </QueryClientProvider>,
  );
}

describe("TorrentDetail stall callout", () => {
  beforeEach(() => {
    stallState.value = undefined;
  });

  it("renders the stall callout when /stall returns stalled=true", () => {
    // L2 + no_peers: classic activity-escalation stall (had peers, lost
    // them, no data for ≥2× stall_timeout). The callout should appear
    // and surface both the level label AND the human-readable reason.
    stallState.value = {
      stalled: true,
      level: 2,
      inactive_secs: 360,
      reason: "no_peers",
    };
    renderDetail();
    expect(screen.getByTestId("stall-callout")).toBeInTheDocument();
    expect(screen.getByText("Stalled (Level 2)")).toBeInTheDocument();
    expect(screen.getByText("lost all peers")).toBeInTheDocument();
  });

  it("does NOT render the stall callout when /stall returns stalled=false", () => {
    // The classic "actively downloading, healthy" case. If the conditional
    // render breaks (e.g. someone changes `{stall?.stalled && ...}` to
    // `{stall && ...}` or to `{stall.stalled || ...}`), the callout
    // appears even on healthy torrents.
    stallState.value = {
      stalled: false,
      level: 0,
      inactive_secs: 5,
      reason: "",
    };
    renderDetail();
    expect(screen.queryByTestId("stall-callout")).not.toBeInTheDocument();
  });

  it("does NOT render the stall callout when /stall is still loading (no data)", () => {
    // First render before the polling query resolves. The callout
    // should stay hidden — showing a "stalled" banner momentarily on
    // every page load would be wrong.
    stallState.value = undefined;
    renderDetail();
    expect(screen.queryByTestId("stall-callout")).not.toBeInTheDocument();
  });
});
