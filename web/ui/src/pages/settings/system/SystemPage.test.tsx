import { afterEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import SystemPage from "./SystemPage";

function renderPage() {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.endsWith("/system/status"))
        return new Response(
          JSON.stringify({ app_name: "Haul", version: "2a37668", build_time: "", go_version: "go1.25", db_type: "sqlite", db_path: "/config/haul.db", uptime_seconds: 7260, start_time: "" }),
          { status: 200 },
        );
      if (url.endsWith("/health"))
        return new Response(
          JSON.stringify({ engine_status: "healthy", vpn_active: true, external_ip: "1.2.3.4", peers_connected: 12, stalled_count: 0, disk_total_bytes: 2 * 1024 ** 4, disk_free_bytes: 1024 ** 4 }),
          { status: 200 },
        );
      return new Response(JSON.stringify({}), { status: 200 });
    }),
  );
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <MemoryRouter>
        <SystemPage />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

afterEach(() => vi.unstubAllGlobals());

describe("Haul System page", () => {
  it("has the same About card as the other apps, then health and maintenance", async () => {
    renderPage();
    expect(await screen.findByText("2a37668")).toBeInTheDocument();
    expect(screen.getByText("2h 1m")).toBeInTheDocument();
    expect(screen.getByText("/config/haul.db")).toBeInTheDocument();
    expect(screen.getByText("Check for updates")).toBeInTheDocument();
    expect(await screen.findByText("Protected")).toBeInTheDocument();
    expect(screen.getByText(/1\.00 TB used of 2\.00 TB/)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /Diagnostics/ })).toHaveAttribute("href", "/system/diagnostics");
    expect(screen.getByRole("link", { name: /Cleanup history/ })).toHaveAttribute("href", "/system/cleanup-history");
  });
});
