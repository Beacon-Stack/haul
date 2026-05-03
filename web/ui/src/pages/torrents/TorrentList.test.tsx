// TorrentList tests pin the dashboard pill counter — specifically the
// "Stalled N" pill at the top of the list. Before the fix, this counter
// silently read 0 even when /api/v1/stalls listed N torrents, because
// the per-row Info.Stalled was always false for the pre-metadata
// no_peers_ever case (the headline dead-magnet bug).
//
// The counter is computed client-side via countForStatus(t, "stalled")
// over t.stalled. This test asserts that when the backend marks a
// torrent stalled=true, the pill renders the matching count.

import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { ConfirmProvider } from "@beacon-shared/ConfirmDialog";
import type { TorrentInfo } from "@/api/torrents";

const torrentState: { value: TorrentInfo[] } = { value: [] };

function makeTorrent(overrides: Partial<TorrentInfo>): TorrentInfo {
  return {
    info_hash: overrides.info_hash ?? Math.random().toString(36).slice(2),
    name: overrides.name ?? "torrent",
    status: overrides.status ?? "downloading",
    save_path: "/tmp",
    category: "",
    tags: null,
    size: 1000,
    downloaded: 0,
    uploaded: 0,
    progress: 0,
    download_rate: 0,
    upload_rate: 0,
    seeds: 0,
    peers: 0,
    seed_ratio: 0,
    eta: 0,
    added_at: new Date().toISOString(),
    content_path: "/tmp/torrent",
    sequential: false,
    stalled: false,
    ...overrides,
  };
}

vi.mock("@/api/torrents", async () => {
  const actual = await vi.importActual<typeof import("@/api/torrents")>("@/api/torrents");
  return {
    ...actual,
    useTorrents: () => ({ data: torrentState.value, isLoading: false }),
    useAddTorrent: () => ({ mutate: vi.fn(), isPending: false }),
    useDeleteTorrent: () => ({ mutate: vi.fn() }),
    usePauseTorrent: () => ({ mutate: vi.fn() }),
    useResumeTorrent: () => ({ mutate: vi.fn() }),
    useReorderTorrents: () => ({ mutate: vi.fn() }),
    useSetTorrentPriority: () => ({ mutate: vi.fn() }),
  };
});

vi.mock("@/api/health", () => ({
  useHealth: () => ({ data: undefined }),
}));

vi.mock("@/api/settings", () => ({
  useSettings: () => ({ data: {} }),
}));

import TorrentList from "./TorrentList";

function renderList() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <ConfirmProvider>
        <MemoryRouter>
          <TorrentList />
        </MemoryRouter>
      </ConfirmProvider>
    </QueryClientProvider>,
  );
}

// pillCount finds the "Stalled" filter-pill button and reads the trailing
// count number — the layout is `[dot] Stalled <N>` so the last text node
// in the button is the count. Targeting via accessible name keeps this
// test resilient to wrapper/styling changes.
function stalledPillCount(): number {
  // Multiple buttons start with "Stalled" — be specific by anchoring on
  // the exact label-then-number pattern. Filter to the one that contains
  // the count we're looking for.
  const buttons = screen.getAllByRole("button");
  const pill = buttons.find((b) => /^\s*Stalled\s*\d+\s*$/.test(b.textContent ?? ""));
  if (!pill) {
    throw new Error(
      "Stalled filter pill not found among buttons: " +
        buttons.map((b) => JSON.stringify(b.textContent)).join(", "),
    );
  }
  const m = pill.textContent?.match(/Stalled\s*(\d+)/);
  return m ? Number(m[1]) : NaN;
}

describe("TorrentList Stalled filter pill", () => {
  it("counts 0 when no torrents are stalled", () => {
    // Two healthy downloading torrents and one paused — none with the
    // backend-set stalled flag. The pill must read 0, not "−" or "0 of N".
    torrentState.value = [
      makeTorrent({ info_hash: "h1", status: "downloading", stalled: false }),
      makeTorrent({ info_hash: "h2", status: "downloading", stalled: false }),
      makeTorrent({ info_hash: "h3", status: "paused" }),
    ];
    renderList();
    expect(stalledPillCount()).toBe(0);
  });

  it("REGRESSION: counts every torrent the backend has classified stalled=true", () => {
    // Two stalled downloading torrents (one pre-metadata no_peers_ever,
    // one post-metadata no_data_received) plus a healthy peer. Before
    // the backend fix, classifyStalled returned false for the pre-metadata
    // case, so this count was off-by-one. Plus a paused torrent that
    // would be ignored because matchesStatus("stalled") requires
    // status === "downloading".
    torrentState.value = [
      makeTorrent({ info_hash: "dead", name: "dead-magnet", status: "downloading", stalled: true }),
      makeTorrent({ info_hash: "stuck", name: "stuck-download", status: "downloading", stalled: true }),
      makeTorrent({ info_hash: "healthy", status: "downloading", stalled: false }),
      makeTorrent({ info_hash: "rest", status: "paused", stalled: false }),
    ];
    renderList();
    expect(stalledPillCount()).toBe(2);
  });

  it("ignores stalled=true on non-downloading torrents", () => {
    // Defensive — matchesStatus("stalled") is `t.status === "downloading"
    // && t.stalled`. If someone removes the status guard, a paused-with-
    // stale-flag torrent would get double-counted under both Paused and
    // Stalled, which would be confusing.
    torrentState.value = [
      makeTorrent({ info_hash: "p", status: "paused", stalled: true }),
      makeTorrent({ info_hash: "s", status: "seeding", stalled: true }),
    ];
    renderList();
    expect(stalledPillCount()).toBe(0);
  });
});
