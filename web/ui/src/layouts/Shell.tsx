import { useEffect, useState } from "react";
import { Outlet, NavLink, Link } from "react-router-dom";
import {
  LayoutDashboard,
  Settings,
  Anchor,
  Activity,
  FolderOpen,
  Rss,
  FileText,
  Menu,
  X,
  ChevronLeft,
  ChevronRight,
} from "lucide-react";
import { useWebSocket } from "@/api/websocket";
import { applyTheme } from "@/theme";

interface NavItem {
  to: string;
  icon: React.ElementType;
  label: string;
}

const NAV: NavItem[] = [
  { to: "/", icon: LayoutDashboard, label: "Dashboard" },
  { to: "/activity", icon: Activity, label: "Activity" },
  { to: "/categories", icon: FolderOpen, label: "Categories" },
  { to: "/media-management", icon: FileText, label: "Media Mgmt" },
  { to: "/rss", icon: Rss, label: "RSS Feeds" },
  { to: "/settings", icon: Settings, label: "Settings" },
];

// Viewport tiers, in order from narrowest to widest:
//
//   mobile   <768px   slide-out drawer + hamburger top bar
//   compact  768-1100 sidebar force-collapsed to icons-only
//   wide     >=1100px sidebar honors saved expanded/collapsed pref
//
// Mirrors the pattern in Pilot/Prism/Pulse so behavior is consistent
// across the Beacon Stack.
type ViewportMode = "mobile" | "compact" | "wide";

function computeViewportMode(): ViewportMode {
  if (typeof window === "undefined") return "wide";
  if (window.innerWidth < 768) return "mobile";
  if (window.innerWidth < 1100) return "compact";
  return "wide";
}

function useViewportMode(): ViewportMode {
  const [mode, setMode] = useState<ViewportMode>(computeViewportMode);
  useEffect(() => {
    const handler = () => setMode(computeViewportMode());
    const mqMobile = window.matchMedia("(max-width: 767px)");
    const mqCompact = window.matchMedia("(max-width: 1099px)");
    mqMobile.addEventListener("change", handler);
    mqCompact.addEventListener("change", handler);
    return () => {
      mqMobile.removeEventListener("change", handler);
      mqCompact.removeEventListener("change", handler);
    };
  }, []);
  return mode;
}

function SidebarNavItem({
  item,
  collapsed,
  onClick,
}: {
  item: NavItem;
  collapsed: boolean;
  onClick?: () => void;
}) {
  const Icon = item.icon;
  return (
    <NavLink
      to={item.to}
      end={item.to === "/"}
      title={item.label}
      onClick={onClick}
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
        whiteSpace: "nowrap",
        overflow: "hidden",
        justifyContent: collapsed ? "center" : "flex-start",
      })}
    >
      <Icon size={16} style={{ flexShrink: 0 }} />
      {!collapsed && (
        <span
          style={{
            overflow: "hidden",
            textOverflow: "ellipsis",
            minWidth: 0,
          }}
        >
          {item.label}
        </span>
      )}
    </NavLink>
  );
}

function Sidebar({
  collapsed,
  onCollapse,
  onClose,
  isMobile,
  autoCollapsed,
}: {
  collapsed: boolean;
  onCollapse: () => void;
  onClose: () => void;
  isMobile: boolean;
  autoCollapsed: boolean;
}) {
  const width = isMobile ? 200 : collapsed ? 60 : 200;

  return (
    <nav
      style={{
        width,
        minWidth: width,
        maxWidth: width,
        flexShrink: 0,
        background: "var(--color-bg-surface)",
        borderRight: "1px solid var(--color-border-subtle)",
        display: "flex",
        flexDirection: "column",
        padding: "16px 0",
        transition: "width 200ms ease, min-width 200ms ease, max-width 200ms ease",
        // The OUTER wrapper in Shell handles positioning. Keeping
        // position:static here lets the wrapper's transform (used to
        // slide the drawer in/out on mobile) actually take effect.
        height: isMobile ? "100vh" : "auto",
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
          justifyContent: collapsed && !isMobile ? "center" : "flex-start",
        }}
      >
        <Link
          to="/"
          style={{
            display: "flex",
            alignItems: "center",
            gap: 8,
            textDecoration: "none",
          }}
        >
          <Anchor size={20} style={{ color: "var(--color-accent)" }} />
          {(!collapsed || isMobile) && (
            <span
              style={{
                fontSize: 16,
                fontWeight: 700,
                letterSpacing: "-0.02em",
                color: "var(--color-text-primary)",
              }}
            >
              Haul
            </span>
          )}
        </Link>
        {isMobile && (
          <button
            onClick={onClose}
            style={{
              marginLeft: "auto",
              background: "none",
              border: "none",
              cursor: "pointer",
              color: "var(--color-text-muted)",
              display: "flex",
              alignItems: "center",
              padding: 4,
            }}
            title="Close menu"
          >
            <X size={18} />
          </button>
        )}
      </div>

      <div style={{ flex: 1, overflowY: "auto" }}>
        {NAV.map((item) => (
          <SidebarNavItem
            key={item.to}
            item={item}
            collapsed={!isMobile && collapsed}
            onClick={isMobile ? onClose : undefined}
          />
        ))}
      </div>

      {/* Manual collapse toggle — hidden in mobile (the X above replaces
          it) and in compact (the gate is automatic). */}
      {!isMobile && !autoCollapsed && (
        <div
          style={{
            borderTop: "1px solid var(--color-border-subtle)",
            padding: 8,
          }}
        >
          <button
            onClick={onCollapse}
            title={collapsed ? "Expand sidebar" : "Collapse sidebar"}
            style={{
              display: "flex",
              alignItems: "center",
              justifyContent: collapsed ? "center" : "flex-end",
              width: "100%",
              padding: "0 12px",
              height: 32,
              background: "none",
              border: "none",
              cursor: "pointer",
              color: "var(--color-text-muted)",
              borderRadius: 6,
            }}
          >
            {collapsed ? <ChevronRight size={14} /> : <ChevronLeft size={14} />}
          </button>
        </div>
      )}
    </nav>
  );
}

export default function Shell() {
  useWebSocket();
  useEffect(() => {
    applyTheme();
  }, []);

  const [userCollapsed, setUserCollapsed] = useState(() => {
    return localStorage.getItem("haul-sidebar-collapsed") === "true";
  });
  const [mobileOpen, setMobileOpen] = useState(false);
  const mode = useViewportMode();
  const isMobile = mode === "mobile";

  // In compact mode (768–1100px) the sidebar is force-collapsed
  // regardless of saved preference.
  const collapsed = mode === "compact" ? true : userCollapsed;

  useEffect(() => {
    if (!isMobile) setMobileOpen(false);
  }, [isMobile]);

  useEffect(() => {
    localStorage.setItem("haul-sidebar-collapsed", String(userCollapsed));
  }, [userCollapsed]);

  return (
    <div style={{ display: "flex", minHeight: "100vh" }}>
      {/* Mobile overlay backdrop */}
      {isMobile && mobileOpen && (
        <div
          onClick={() => setMobileOpen(false)}
          style={{
            position: "fixed",
            inset: 0,
            background: "rgba(0, 0, 0, 0.5)",
            zIndex: 40,
          }}
        />
      )}

      {/* Sidebar (slides in on mobile, otherwise inline) */}
      <div
        style={{
          transform: isMobile
            ? mobileOpen
              ? "translateX(0)"
              : "translateX(-100%)"
            : "none",
          transition: "transform 200ms ease",
          position: isMobile ? "fixed" : "relative",
          top: 0,
          left: 0,
          zIndex: 50,
        }}
      >
        <Sidebar
          collapsed={collapsed}
          onCollapse={() => setUserCollapsed((c) => !c)}
          onClose={() => setMobileOpen(false)}
          isMobile={isMobile}
          autoCollapsed={mode === "compact"}
        />
      </div>

      {/* Content area */}
      <main
        style={{
          flex: 1,
          marginLeft: isMobile ? 0 : 0, // sidebar is now inline, not fixed
          minWidth: 0,
          display: "flex",
          flexDirection: "column",
        }}
      >
        {/* Mobile top bar with hamburger */}
        {isMobile && (
          <div
            style={{
              height: 52,
              borderBottom: "1px solid var(--color-border-subtle)",
              display: "flex",
              alignItems: "center",
              padding: "0 12px",
              gap: 8,
              background: "var(--color-bg-surface)",
              flexShrink: 0,
            }}
          >
            <button
              onClick={() => setMobileOpen(true)}
              style={{
                background: "none",
                border: "none",
                cursor: "pointer",
                color: "var(--color-text-primary)",
                display: "flex",
                alignItems: "center",
                padding: 6,
              }}
              title="Open menu"
            >
              <Menu size={20} />
            </button>
            <Anchor size={18} style={{ color: "var(--color-accent)" }} />
            <span
              style={{
                fontSize: 15,
                fontWeight: 700,
                letterSpacing: "-0.02em",
                color: "var(--color-text-primary)",
              }}
            >
              Haul
            </span>
          </div>
        )}
        <div style={{ flex: 1, overflow: "auto" }}>
          <Outlet />
        </div>
      </main>
    </div>
  );
}

