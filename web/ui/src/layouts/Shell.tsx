import { useEffect } from "react";
import {
  Activity,
  Anchor,
  FileText,
  FolderOpen,
  LayoutDashboard,
  Rss,
  Settings,
} from "lucide-react";
import Shell, { type NavItem } from "@beacon-shared/Shell";
import { useWebSocket } from "@/api/websocket";
import { applyTheme } from "@/theme";

const mainNav: NavItem[] = [
  { to: "/", icon: LayoutDashboard, label: "Dashboard" },
  { to: "/activity", icon: Activity, label: "Activity" },
  { to: "/categories", icon: FolderOpen, label: "Categories" },
  { to: "/media-management", icon: FileText, label: "Media Mgmt" },
  { to: "/rss", icon: Rss, label: "RSS Feeds" },
  { to: "/settings", icon: Settings, label: "Settings" },
];

// AppIcon — Haul uses a bare anchor glyph (no accent-color tile) to
// signal "lower-level utility" vs the framed app icons of the manager
// services.
function AppIcon() {
  return <Anchor size={20} style={{ color: "var(--color-accent)" }} />;
}

export default function HaulShell() {
  useWebSocket();
  useEffect(() => {
    applyTheme();
  }, []);

  return (
    <Shell
      appName="Haul"
      appIcon={<AppIcon />}
      mainNav={mainNav}
      collapsedStorageKey="haul-sidebar-collapsed"
    />
  );
}
