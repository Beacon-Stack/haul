import { useEffect } from "react";
import { Outlet, NavLink } from "react-router-dom";
import { LayoutDashboard, Settings, Anchor, Activity, FolderOpen, Rss, FileText } from "lucide-react";
import { useWebSocket } from "@/api/websocket";
import { applyTheme } from "@/theme";

const NAV = [
  { to: "/", icon: LayoutDashboard, label: "Dashboard" },
  { to: "/activity", icon: Activity, label: "Activity" },
  { to: "/categories", icon: FolderOpen, label: "Categories" },
  { to: "/media-management", icon: FileText, label: "Media Mgmt" },
  { to: "/rss", icon: Rss, label: "RSS Feeds" },
  { to: "/settings", icon: Settings, label: "Settings" },
];

export default function Shell() {
  useWebSocket();
  useEffect(() => { applyTheme(); }, []);

  return (
    <div style={{ display: "flex", minHeight: "100vh" }}>
      {/* Sidebar */}
      <nav
        style={{
          width: 200,
          flexShrink: 0,
          background: "var(--color-bg-surface)",
          borderRight: "1px solid var(--color-border-subtle)",
          display: "flex",
          flexDirection: "column",
          padding: "16px 0",
        }}
      >
        {/* Logo */}
        <div
          style={{
            display: "flex",
            alignItems: "center",
            gap: 8,
            padding: "0 16px 20px",
            borderBottom: "1px solid var(--color-border-subtle)",
            marginBottom: 12,
          }}
        >
          <Anchor size={20} style={{ color: "var(--color-accent)" }} />
          <span style={{ fontSize: 16, fontWeight: 700, letterSpacing: "-0.02em", color: "var(--color-text-primary)" }}>
            Haul
          </span>
        </div>

        {NAV.map(({ to, icon: Icon, label }) => (
          <NavLink
            key={to}
            to={to}
            end={to === "/"}
            style={({ isActive }) => ({
              display: "flex",
              alignItems: "center",
              gap: 10,
              padding: "8px 16px",
              margin: "2px 8px",
              borderRadius: 6,
              fontSize: 13,
              fontWeight: isActive ? 600 : 400,
              color: isActive ? "var(--color-accent)" : "var(--color-text-secondary)",
              background: isActive ? "var(--color-accent-muted)" : "transparent",
              textDecoration: "none",
            })}
          >
            <Icon size={16} />
            {label}
          </NavLink>
        ))}
      </nav>

      {/* Content */}
      <main style={{ flex: 1, overflow: "auto" }}>
        <Outlet />
      </main>
    </div>
  );
}
