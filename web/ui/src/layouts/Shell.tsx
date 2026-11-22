import { useEffect } from "react";
import {
  Activity,
  Anchor,
  FileText,
  FolderOpen,
  LayoutDashboard,
  Settings,
  Server,
} from "lucide-react";
import Shell, { type NavItem } from "@beacon-shared/Shell";
import UpdatedNotice from "@beacon-shared/UpdatedNotice";
import { useWebSocket } from "@/api/websocket";
import { applyTheme } from "@/theme";

// nav is the static sidebar. System and Logs sit where they do in every
// Beacon app; diagnostics are linked from the System page.
const nav: NavItem[] = [
  { to: "/", icon: LayoutDashboard, label: "Dashboard" },
  { to: "/activity", icon: Activity, label: "Activity" },
  { to: "/categories", icon: FolderOpen, label: "Categories" },
  { to: "/media-management", icon: FileText, label: "Media Mgmt" },
  { to: "/settings/system", icon: Server, label: "System" },
  { to: "/settings/system/logs", icon: FileText, label: "Logs" },
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
      overlay={<UpdatedNotice app="Haul" />}
    />
  );
}
