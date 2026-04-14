import { Rss } from "lucide-react";

export default function RSSPage() {
  return (
    <div style={{ padding: 24, maxWidth: 700, margin: "0 auto" }}>
      <div style={{ marginBottom: 24 }}>
        <h1 style={{ fontSize: 20, fontWeight: 600, color: "var(--color-text-primary)", margin: 0 }}>RSS Feeds</h1>
        <p style={{ fontSize: 13, color: "var(--color-text-secondary)", margin: "4px 0 0" }}>
          Monitor RSS feeds and automatically download matching torrents.
        </p>
      </div>

      <div style={{ textAlign: "center", padding: "60px 0" }}>
        <Rss size={32} style={{ color: "var(--color-text-muted)", marginBottom: 12 }} />
        <p style={{ fontSize: 14, color: "var(--color-text-secondary)", fontWeight: 500 }}>Coming soon</p>
        <p style={{ fontSize: 13, color: "var(--color-text-muted)", margin: "6px 0 0" }}>
          RSS feed monitoring with auto-download rules is planned for a future release.
        </p>
      </div>
    </div>
  );
}
