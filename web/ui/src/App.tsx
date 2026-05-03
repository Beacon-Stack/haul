import { BrowserRouter, Routes, Route } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { Toaster } from "sonner";
import { ConfirmProvider } from "@beacon-shared/ConfirmDialog";
import Shell from "@/layouts/Shell";
import TorrentList from "@/pages/torrents/TorrentList";
import TorrentDetail from "@/pages/torrents/TorrentDetail";
import ActivityPage from "@/pages/activity/ActivityPage";
import CategoriesPage from "@/pages/categories/CategoriesPage";
import RSSPage from "@/pages/rss/RSSPage";
import SettingsPage from "@/pages/settings/SettingsPage";
import MediaManagementPage from "@/pages/media-management/MediaManagementPage";
import DiagnosticsPage from "@/pages/system/Diagnostics";
import CleanupHistoryPage from "@/pages/system/CleanupHistory";

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: 1, staleTime: 5000 } },
});

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <ConfirmProvider>
        <BrowserRouter>
          <Routes>
            <Route element={<Shell />}>
              <Route index element={<TorrentList />} />
              <Route path="torrents/:hash" element={<TorrentDetail />} />
              <Route path="activity" element={<ActivityPage />} />
              <Route path="categories" element={<CategoriesPage />} />
              <Route path="rss" element={<RSSPage />} />
              <Route path="media-management" element={<MediaManagementPage />} />
              <Route path="settings" element={<SettingsPage />} />
              <Route path="system/diagnostics" element={<DiagnosticsPage />} />
              <Route path="system/cleanup-history" element={<CleanupHistoryPage />} />
            </Route>
          </Routes>
          <Toaster
            position="bottom-right"
            toastOptions={{
              style: {
                background: "var(--color-bg-elevated)",
                border: "1px solid var(--color-border-default)",
                color: "var(--color-text-primary)",
                fontSize: 13,
              },
            }}
          />
        </BrowserRouter>
      </ConfirmProvider>
    </QueryClientProvider>
  );
}
