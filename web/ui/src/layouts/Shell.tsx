import { useEffect } from "react";
import {
  Activity,
  Anchor,
  FileText,
  FolderOpen,
  LayoutDashboard,
  Settings,
  Stethoscope,
} from "lucide-react";
import Shell, { type NavItem } from "@beacon-shared/Shell";
import { useWebSocket } from "@/api/websocket";
import { applyTheme } from "@/theme";

// nav is the static sidebar. System lives below Settings — it's the
// admin-only escape hatch for inspecting/cleaning DB rows that the
// regular UI doesn't surface. Always shown; the page itself explains
// what to do if the HAUL_ADMIN_DIAGNOSTICS_ENABLED flag is off.
const nav: NavItem[] = [
  { to: "/", icon: LayoutDashboard, label: "Dashboard" },
  { to: "/activity", icon: Activity, label: "Activity" },
  { to: "/categories", icon: FolderOpen, label: "Categories" },
  { to: "/media-management", icon: FileText, label: "Media Mgmt" },
  { to: "/system/diagnostics", icon: Stethoscope, label: "System" },
  { to: "/system/logs", icon: FileText, label: "Logs" },
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
      mainNav={nav}
      collapsedStorageKey="haul-sidebar-collapsed"
    />
  );
}
