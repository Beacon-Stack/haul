import { useState, useEffect } from "react";
import { useStats } from "@/api/stats";
import { useHealth } from "@/api/health";
import { useSettings, useSaveSettings, type SettingsMap } from "@/api/settings";
import { toast } from "sonner";
import {
  Settings, HardDrive, Activity, Wifi, Download, Upload,
  Zap, Shield, Clock, Terminal, Anchor, Monitor, Moon, Sun, Check, ToggleLeft,
} from "lucide-react";
import {
  THEME_PRESETS,
  getStoredMode,
  getStoredPreset,
  resolveMode,
  setThemeMode,
  setThemePreset,
} from "@/theme";
import type { ThemeMode } from "@/theme";

// ── Shared styles ─────────────────────────────────────────────────────────

const inputStyle: React.CSSProperties = {
  width: "100%",
  background: "var(--color-bg-elevated)",
  border: "1px solid var(--color-border-default)",
  borderRadius: 6,
  padding: "8px 12px",
  fontSize: 13,
  color: "var(--color-text-primary)",
  outline: "none",
  boxSizing: "border-box",
};

function onInputFocus(e: React.FocusEvent<HTMLInputElement | HTMLSelectElement>) {
  e.currentTarget.style.borderColor = "var(--color-accent)";
}
function onInputBlur(e: React.FocusEvent<HTMLInputElement | HTMLSelectElement>) {
  e.currentTarget.style.borderColor = "var(--color-border-default)";
}

// ── Appearance ────────────────────────────────────────────────────────────

function AppearanceSection() {
  const [mode, setMode] = useState<ThemeMode>(getStoredMode);
  const resolved = resolveMode(mode);
  const [darkPreset, setDarkPreset] = useState(() => getStoredPreset("dark"));
  const [lightPreset, setLightPreset] = useState(() => getStoredPreset("light"));

  const currentPresetId = resolved === "dark" ? darkPreset : lightPreset;

  function handleModeChange(next: ThemeMode) {
    setMode(next);
    setThemeMode(next);
  }

  function handlePresetSelect(presetId: string, presetMode: "dark" | "light") {
    if (presetMode === "dark") setDarkPreset(presetId);
    else setLightPreset(presetId);
    setThemePreset(presetMode, presetId);
  }

  const darkPresets = THEME_PRESETS.filter((p) => p.mode === "dark");
  const lightPresets = THEME_PRESETS.filter((p) => p.mode === "light");

  const modeButtons: { m: ThemeMode; Icon: React.ElementType; label: string }[] = [
    { m: "dark", Icon: Moon, label: "Dark" },
    { m: "light", Icon: Sun, label: "Light" },
    { m: "system", Icon: Monitor, label: "System" },
  ];

  const presetGrid = (presets: typeof THEME_PRESETS) => (
    <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fill, minmax(130px, 1fr))", gap: 10, marginTop: 12 }}>
      {presets.map((preset) => {
        const selected = preset.id === currentPresetId;
        return (
          <button
            key={preset.id}
            onClick={() => handlePresetSelect(preset.id, preset.mode)}
            title={preset.label}
            style={{
              display: "flex",
              flexDirection: "column",
              gap: 0,
              borderRadius: 8,
              border: selected ? "2px solid var(--color-accent)" : "2px solid var(--color-border-subtle)",
              overflow: "hidden",
              cursor: "pointer",
              background: "none",
              padding: 0,
              transition: "border-color 120ms ease, box-shadow 120ms ease",
              boxShadow: selected ? "0 0 0 1px var(--color-accent)" : "none",
            }}
            onMouseEnter={(e) => { if (!selected) (e.currentTarget as HTMLButtonElement).style.borderColor = "var(--color-border-strong)"; }}
            onMouseLeave={(e) => { if (!selected) (e.currentTarget as HTMLButtonElement).style.borderColor = "var(--color-border-subtle)"; }}
          >
            <div style={{ display: "flex", height: 40, position: "relative" }}>
              <div style={{ flex: 1, background: preset.preview.bg }} />
              <div style={{ flex: 1, background: preset.preview.surface }} />
              <div style={{ width: 12, background: preset.preview.accent, flexShrink: 0 }} />
              {selected && (
                <div style={{ position: "absolute", inset: 0, display: "flex", alignItems: "center", justifyContent: "center", background: "rgba(0,0,0,0.30)" }}>
                  <Check size={16} strokeWidth={2.5} color="#fff" />
                </div>
              )}
            </div>
            <div style={{ padding: "6px 8px", background: preset.preview.surface, display: "flex", alignItems: "center", gap: 6 }}>
              {selected && <span style={{ width: 6, height: 6, borderRadius: "50%", background: preset.preview.accent, flexShrink: 0 }} />}
              <span style={{ fontSize: 11, fontWeight: selected ? 600 : 500, color: preset.preview.text, whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis", minWidth: 0 }}>
                {preset.label}
              </span>
            </div>
          </button>
        );
      })}
    </div>
  );

  return (
    <div style={{ background: "var(--color-bg-surface)", border: "1px solid var(--color-border-subtle)", borderRadius: 10, overflow: "hidden" }}>
      <div style={{ padding: "14px 20px", borderBottom: "1px solid var(--color-border-subtle)", background: "var(--color-bg-elevated)", display: "flex", alignItems: "center", gap: 10 }}>
        <Sun size={15} style={{ color: "var(--color-accent)" }} />
        <span style={{ fontSize: 13, fontWeight: 600, color: "var(--color-accent)", letterSpacing: "0.01em" }}>Appearance</span>
      </div>
      <div style={{ padding: 20, display: "flex", flexDirection: "column", gap: 20 }}>
        <div>
          <span style={{ display: "block", fontSize: 13, fontWeight: 500, color: "var(--color-text-primary)", marginBottom: 10 }}>
            Color mode
          </span>
          <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
            {modeButtons.map(({ m, Icon, label }) => {
              const active = mode === m;
              return (
                <button
                  key={m}
                  onClick={() => handleModeChange(m)}
                  style={{
                    display: "flex", alignItems: "center", gap: 6, padding: "6px 14px", borderRadius: 6,
                    border: active ? "1px solid var(--color-accent)" : "1px solid var(--color-border-default)",
                    background: active ? "var(--color-accent-muted)" : "var(--color-bg-elevated)",
                    color: active ? "var(--color-accent-hover)" : "var(--color-text-secondary)",
                    fontSize: 13, fontWeight: 500, cursor: "pointer",
                    transition: "background 120ms ease, border-color 120ms ease, color 120ms ease",
                  }}
                  onMouseEnter={(e) => { if (!active) { (e.currentTarget as HTMLButtonElement).style.borderColor = "var(--color-border-strong)"; (e.currentTarget as HTMLButtonElement).style.color = "var(--color-text-primary)"; } }}
                  onMouseLeave={(e) => { if (!active) { (e.currentTarget as HTMLButtonElement).style.borderColor = "var(--color-border-default)"; (e.currentTarget as HTMLButtonElement).style.color = "var(--color-text-secondary)"; } }}
                >
                  <Icon size={14} strokeWidth={2} />
                  {label}
                </button>
              );
            })}
          </div>
        </div>

        {(mode === "dark" || mode === "system") && (
          <div>
            {mode === "system" && <span style={{ display: "block", fontSize: 12, fontWeight: 500, color: "var(--color-text-secondary)", marginBottom: 4 }}>Dark theme</span>}
            {presetGrid(darkPresets)}
          </div>
        )}
        {(mode === "light" || mode === "system") && (
          <div>
            {mode === "system" && <span style={{ display: "block", fontSize: 12, fontWeight: 500, color: "var(--color-text-secondary)", marginBottom: 4 }}>Light theme</span>}
            {presetGrid(lightPresets)}
          </div>
        )}
      </div>
    </div>
  );
}

// ── Reusable components ───────────────────────────────────────────────────

function SectionCard({ title, icon: Icon, children, advanced }: {
  title: string; icon: React.ElementType; children: React.ReactNode; advanced?: React.ReactNode;
}) {
  const [showAdvanced, setShowAdvanced] = useState(false);

  return (
    <div style={{
      background: "var(--color-bg-surface)",
      border: "1px solid var(--color-border-subtle)",
      borderRadius: 10,
      overflow: "hidden",
    }}>
      <div style={{
        padding: "14px 20px",
        borderBottom: "1px solid var(--color-border-subtle)",
        background: "var(--color-bg-elevated)",
        display: "flex",
        alignItems: "center",
        gap: 10,
      }}>
        <Icon size={15} style={{ color: "var(--color-accent)" }} />
        <span style={{ fontSize: 13, fontWeight: 600, color: "var(--color-accent)", letterSpacing: "0.01em" }}>
          {title}
        </span>
      </div>
      <div style={{ padding: 20, display: "flex", flexDirection: "column", gap: 20 }}>
        {children}

        {advanced && showAdvanced && (
          <>
            <div style={{ height: 1, background: "var(--color-border-subtle)", margin: "0 -20px", width: "calc(100% + 40px)" }} />
            {advanced}
          </>
        )}

        {advanced && (
          <button
            onClick={() => setShowAdvanced((v) => !v)}
            style={{
              alignSelf: "flex-start",
              background: "none",
              border: "none",
              color: "var(--color-text-muted)",
              fontSize: 11,
              cursor: "pointer",
              padding: 0,
              display: "flex",
              alignItems: "center",
              gap: 4,
            }}
            onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.color = "var(--color-text-secondary)"; }}
            onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.color = "var(--color-text-muted)"; }}
          >
            {showAdvanced ? "▾ Hide advanced" : "▸ Show advanced"}
          </button>
        )}
      </div>
    </div>
  );
}

function ToggleRow({ label, description, checked, onChange }: {
  label: string; description: string; checked: boolean; onChange: (v: boolean) => void;
}) {
  return (
    <div style={{
      display: "flex",
      alignItems: "center",
      justifyContent: "space-between",
      gap: 16,
      paddingBottom: 16,
      borderBottom: "1px solid var(--color-border-subtle)",
    }}>
      <div>
        <span style={{ display: "block", fontSize: 13, fontWeight: 500, color: "var(--color-text-primary)", marginBottom: 2 }}>
          {label}
        </span>
        <span style={{ fontSize: 12, color: "var(--color-text-muted)" }}>{description}</span>
      </div>
      <button
        role="switch"
        aria-checked={checked}
        onClick={() => onChange(!checked)}
        style={{
          width: 40,
          height: 22,
          borderRadius: 11,
          border: "none",
          background: checked ? "var(--color-accent)" : "var(--color-bg-subtle)",
          cursor: "pointer",
          position: "relative",
          flexShrink: 0,
          transition: "background 150ms ease",
        }}
      >
        <span style={{
          position: "absolute",
          top: 3,
          left: checked ? 21 : 3,
          width: 16,
          height: 16,
          borderRadius: "50%",
          background: "var(--color-bg-base)",
          transition: "left 150ms ease",
        }} />
      </button>
    </div>
  );
}

function FieldRow({ label, description, children }: { label: string; description?: string; children: React.ReactNode }) {
  return (
    <div style={{ paddingBottom: 16, borderBottom: "1px solid var(--color-border-subtle)" }}>
      <label style={{ display: "block", fontSize: 12, fontWeight: 500, color: "var(--color-text-secondary)", marginBottom: 6 }}>
        {label}
      </label>
      {description && <p style={{ margin: "0 0 6px", fontSize: 11, color: "var(--color-text-muted)" }}>{description}</p>}
      {children}
    </div>
  );
}

function formatBytes(b: number): string {
  if (b <= 0) return "0 B";
  if (b < 1024 * 1024 * 1024) return `${(b / (1024 * 1024 * 1024)).toFixed(1)} GB`;
  return `${(b / (1024 * 1024 * 1024 * 1024)).toFixed(2)} TB`;
}

// ── Settings Page ─────────────────────────────────────────────────────────

export default function SettingsPage() {
  const { data: stats } = useStats();
  const { data: health } = useHealth();
  const { data: savedSettings } = useSettings();
  const saveSettings = useSaveSettings();

  // Local form state — initialized from saved settings.
  const [form, setForm] = useState<SettingsMap>({});
  const [dirty, setDirty] = useState(false);

  useEffect(() => {
    if (savedSettings) {
      setForm(savedSettings);
      setDirty(false);
    }
  }, [savedSettings]);

  function set(key: string, value: string) {
    setForm((prev) => ({ ...prev, [key]: value }));
    setDirty(true);
  }

  function getBool(key: string, fallback = false): boolean {
    const v = form[key];
    if (v === undefined) return fallback;
    return v === "true" || v === "1";
  }

  function getNum(key: string, fallback = 0): number {
    const v = form[key];
    if (v === undefined) return fallback;
    const n = Number(v);
    return isNaN(n) ? fallback : n;
  }

  function getStr(key: string, fallback = ""): string {
    return form[key] ?? fallback;
  }

  function handleSave() {
    saveSettings.mutate(form, {
      onSuccess: () => {
        toast.success("Settings saved");
        setDirty(false);
      },
      onError: (e) => toast.error((e as Error).message),
    });
  }

  const diskPercent = health && health.disk_total_bytes > 0
    ? ((health.disk_total_bytes - health.disk_free_bytes) / health.disk_total_bytes * 100)
    : 0;

  return (
    <div style={{ padding: 24, maxWidth: 1100, margin: "0 auto" }}>
      {/* Header with save button */}
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 24 }}>
        <div>
          <h1 style={{ fontSize: 20, fontWeight: 600, color: "var(--color-text-primary)", margin: 0 }}>Settings</h1>
          <p style={{ fontSize: 13, color: "var(--color-text-secondary)", margin: "4px 0 0" }}>Configure Haul</p>
        </div>
        <button
          onClick={handleSave}
          disabled={!dirty || saveSettings.isPending}
          style={{
            padding: "8px 18px",
            background: dirty ? "var(--color-accent)" : "var(--color-bg-elevated)",
            color: dirty ? "white" : "var(--color-text-muted)",
            border: "none",
            borderRadius: 6,
            fontSize: 13,
            fontWeight: 500,
            cursor: dirty ? "pointer" : "default",
            transition: "all 0.15s",
          }}
          onMouseEnter={(e) => { if (dirty) (e.currentTarget as HTMLButtonElement).style.background = "var(--color-accent-hover)"; }}
          onMouseLeave={(e) => { if (dirty) (e.currentTarget as HTMLButtonElement).style.background = "var(--color-accent)"; }}
        >
          {saveSettings.isPending ? "Saving…" : "Save Changes"}
        </button>
      </div>

      <div style={{ display: "flex", flexDirection: "column", gap: 20 }}>

        {/* Appearance */}
        <AppearanceSection />

        {/* Optional features — pages that ship hidden until the user opts in. */}
        <SectionCard title="Features" icon={ToggleLeft}>
          <ToggleRow
            label="RSS Feeds"
            description="Show the RSS Feeds menu and enable feed-driven auto-grabbing. Off by default — most users have Pilot/Prism (or Sonarr/Radarr) handling automation already, so this is for standalone deployments where you want Haul to subscribe to a feed and grab matches itself."
            checked={getBool("enable_rss_feeds")}
            onChange={(v) => set("enable_rss_feeds", String(v))}
          />
        </SectionCard>

        {/* Row 1: Downloads + Speed */}
        <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 20, alignItems: "start" }}>
        {/* ── Downloads ──────────────────────────────────────────────────── */}
        <SectionCard title="Downloads" icon={Download} advanced={<>
          <FieldRow label="Content Layout" description="How torrent files are organized on disk.">
            <select value={getStr("content_layout", "original")} onChange={(e) => set("content_layout", e.target.value)} style={{ ...inputStyle, fontFamily: "inherit" }} onFocus={onInputFocus} onBlur={onInputBlur}>
              <option value="original">Original — keep file structure as-is</option>
              <option value="subfolder">Subfolder — always create a subfolder</option>
              <option value="no_subfolder">No subfolder — flatten to save path</option>
            </select>
          </FieldRow>

          <ToggleRow
            label="Pre-allocate disk space"
            description="Reserve full file size before downloading. Disable on CoW filesystems (btrfs, ZFS)."
            checked={getBool("pre_allocate")}
            onChange={(v) => set("pre_allocate", String(v))}
          />

          <FieldRow label="Incomplete File Extension" description="Append this extension to files still downloading. Leave empty to disable.">
            <input value={getStr("incomplete_file_ext")} onChange={(e) => set("incomplete_file_ext", e.target.value)} placeholder=".haul" style={inputStyle} onFocus={onInputFocus} onBlur={onInputBlur} />
          </FieldRow>
        </>}>
          <FieldRow label="Default Save Path" description="Where new torrents save their data by default.">
            <input value={getStr("download_dir", "/home/Downloads")} onChange={(e) => set("download_dir", e.target.value)} style={inputStyle} onFocus={onInputFocus} onBlur={onInputBlur} />
          </FieldRow>
        </SectionCard>

        {/* ── Speed ──────────────────────────────────────────────────────── */}
        <SectionCard title="Speed" icon={Zap} advanced={<>
          <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 16 }}>
            <FieldRow label="Alt Download Limit (KB/s)" description="Used during scheduled slow mode">
              <input type="number" min={0} value={getNum("alt_download_limit")} onChange={(e) => set("alt_download_limit", e.target.value)} style={inputStyle} onFocus={onInputFocus} onBlur={onInputBlur} />
            </FieldRow>
            <FieldRow label="Alt Upload Limit (KB/s)" description="Used during scheduled slow mode">
              <input type="number" min={0} value={getNum("alt_upload_limit")} onChange={(e) => set("alt_upload_limit", e.target.value)} style={inputStyle} onFocus={onInputFocus} onBlur={onInputBlur} />
            </FieldRow>
          </div>

          <ToggleRow
            label="Alt speed schedule"
            description="Automatically switch to alt speed limits during a time window."
            checked={getBool("schedule_enabled")}
            onChange={(v) => set("schedule_enabled", String(v))}
          />

          {getBool("schedule_enabled") && (
            <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr 1fr", gap: 16 }}>
              <FieldRow label="From (hour)">
                <input type="number" min={0} max={23} value={getNum("schedule_from_hour", 8)} onChange={(e) => set("schedule_from_hour", e.target.value)} style={inputStyle} onFocus={onInputFocus} onBlur={onInputBlur} />
              </FieldRow>
              <FieldRow label="To (hour)">
                <input type="number" min={0} max={23} value={getNum("schedule_to_hour", 20)} onChange={(e) => set("schedule_to_hour", e.target.value)} style={inputStyle} onFocus={onInputFocus} onBlur={onInputBlur} />
              </FieldRow>
              <FieldRow label="Days">
                <select value={getStr("schedule_days", "all")} onChange={(e) => set("schedule_days", e.target.value)} style={{ ...inputStyle, fontFamily: "inherit" }} onFocus={onInputFocus} onBlur={onInputBlur}>
                  <option value="all">Every day</option>
                  <option value="weekday">Weekdays</option>
                  <option value="weekend">Weekends</option>
                </select>
              </FieldRow>
            </div>
          )}
        </>}>
          <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 16 }}>
            <FieldRow label="Global Download Limit (KB/s)" description="0 = unlimited">
              <input type="number" min={0} value={getNum("global_download_limit")} onChange={(e) => set("global_download_limit", e.target.value)} style={inputStyle} onFocus={onInputFocus} onBlur={onInputBlur} />
            </FieldRow>
            <FieldRow label="Global Upload Limit (KB/s)" description="0 = unlimited">
              <input type="number" min={0} value={getNum("global_upload_limit")} onChange={(e) => set("global_upload_limit", e.target.value)} style={inputStyle} onFocus={onInputFocus} onBlur={onInputBlur} />
            </FieldRow>
          </div>
        </SectionCard>
        </div>

        {/* Row 2: BitTorrent + Seeding */}
        <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 20, alignItems: "start" }}>
        {/* ── BitTorrent ─────────────────────────────────────────────────── */}
        <SectionCard title="BitTorrent" icon={Shield} advanced={<>
          <ToggleRow label="DHT" description="Distributed Hash Table for decentralized peer discovery." checked={getBool("enable_dht", true)} onChange={(v) => set("enable_dht", String(v))} />
          <ToggleRow label="PEX" description="Peer Exchange — learn about peers from other peers." checked={getBool("enable_pex", true)} onChange={(v) => set("enable_pex", String(v))} />
          <ToggleRow label="uTP" description="uTP transport protocol for better NAT traversal." checked={getBool("enable_utp", true)} onChange={(v) => set("enable_utp", String(v))} />
          <ToggleRow label="LSD" description="Local Service Discovery — find peers on your LAN." checked={getBool("enable_lsd", true)} onChange={(v) => set("enable_lsd", String(v))} />
          <ToggleRow label="Announce to all trackers" description="Announce to all trackers in all tiers, not just the first working one." checked={getBool("announce_to_all_trackers")} onChange={(v) => set("announce_to_all_trackers", String(v))} />
        </>}>
          <FieldRow label="Encryption" description="Protocol encryption for peer connections.">
            <select value={getStr("encryption", "prefer")} onChange={(e) => set("encryption", e.target.value)} style={{ ...inputStyle, fontFamily: "inherit" }} onFocus={onInputFocus} onBlur={onInputBlur}>
              <option value="prefer">Prefer — use encryption when available</option>
              <option value="require">Require — only connect with encryption</option>
              <option value="disable">Disable — never use encryption</option>
            </select>
          </FieldRow>
        </SectionCard>

        {/* ── Seeding ────────────────────────────────────────────────────── */}
        <SectionCard title="Seeding" icon={Upload} advanced={<>
          <FieldRow label="Default Seed Time (minutes)" description="0 = unlimited. Stop seeding after this many minutes.">
            <input type="number" min={0} value={getNum("default_seed_time")} onChange={(e) => set("default_seed_time", e.target.value)} style={inputStyle} onFocus={onInputFocus} onBlur={onInputBlur} />
          </FieldRow>
        </>}>
          <ToggleRow
            label="Stop seeding when complete"
            description="Immediately pause torrents when download finishes. No seeding at all."
            checked={getBool("pause_on_complete")}
            onChange={(v) => set("pause_on_complete", String(v))}
          />

          {!getBool("pause_on_complete") && (
            <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 16 }}>
              <FieldRow label="Default Seed Ratio" description="0 = unlimited.">
                <input type="number" min={0} step={0.1} value={getNum("default_seed_ratio")} onChange={(e) => set("default_seed_ratio", e.target.value)} style={inputStyle} onFocus={onInputFocus} onBlur={onInputBlur} />
              </FieldRow>
              <FieldRow label="Action on limit reached">
                <select value={getStr("seed_limit_action", "pause")} onChange={(e) => set("seed_limit_action", e.target.value)} style={{ ...inputStyle, fontFamily: "inherit" }} onFocus={onInputFocus} onBlur={onInputBlur}>
                  <option value="pause">Pause torrent</option>
                  <option value="remove">Remove torrent (keep data)</option>
                  <option value="remove_with_data">Remove torrent + delete data</option>
                </select>
              </FieldRow>
            </div>
          )}
        </SectionCard>
        </div>

        {/* Row 3: Connection + Queue */}
        <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 20, alignItems: "start" }}>
        {/* ── Connection ─────────────────────────────────────────────────── */}
        <SectionCard title="Connection" icon={Wifi} advanced={<>
          <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 16 }}>
            <FieldRow label="Max Global Connections" description="0 = unlimited">
              <input type="number" min={0} value={getNum("max_connections", 500)} onChange={(e) => set("max_connections", e.target.value)} style={inputStyle} onFocus={onInputFocus} onBlur={onInputBlur} />
            </FieldRow>
            <FieldRow label="Max Per-Torrent Connections" description="0 = unlimited">
              <input type="number" min={0} value={getNum("max_connections_per_torrent", 100)} onChange={(e) => set("max_connections_per_torrent", e.target.value)} style={inputStyle} onFocus={onInputFocus} onBlur={onInputBlur} />
            </FieldRow>
          </div>

          <FieldRow label="Network Interface" description="Bind peer connections to a specific interface (e.g. tun0, wg0). Leave empty for all. Requires restart.">
            <input value={getStr("network_interface")} onChange={(e) => set("network_interface", e.target.value)} placeholder="All interfaces" style={inputStyle} onFocus={onInputFocus} onBlur={onInputBlur} />
          </FieldRow>
        </>}>
          <FieldRow label="Listen Port" description="Port for incoming peer connections. Requires restart.">
            <input type="number" min={0} max={65535} value={getNum("listen_port", 6881)} onChange={(e) => set("listen_port", e.target.value)} style={inputStyle} onFocus={onInputFocus} onBlur={onInputBlur} />
          </FieldRow>
        </SectionCard>

        {/* ── Queue ──────────────────────────────────────────────────────── */}
        <SectionCard title="Queue" icon={Clock} advanced={<>
          <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 16 }}>
            <FieldRow label="Max Active Uploads" description="0 = unlimited">
              <input type="number" min={0} value={getNum("max_active_uploads")} onChange={(e) => set("max_active_uploads", e.target.value)} style={inputStyle} onFocus={onInputFocus} onBlur={onInputBlur} />
            </FieldRow>
            <FieldRow label="Max Active Total" description="0 = unlimited">
              <input type="number" min={0} value={getNum("max_active_torrents")} onChange={(e) => set("max_active_torrents", e.target.value)} style={inputStyle} onFocus={onInputFocus} onBlur={onInputBlur} />
            </FieldRow>
          </div>

          <ToggleRow label="Ignore slow torrents" description="Don't count torrents below the speed threshold toward queue limits." checked={getBool("ignore_slow_torrents")} onChange={(v) => set("ignore_slow_torrents", String(v))} />

          <FieldRow label="Slow Torrent Threshold (KB/s)" description="Torrents below this speed are considered slow.">
            <input type="number" min={0} value={getNum("slow_torrent_threshold", 2)} onChange={(e) => set("slow_torrent_threshold", e.target.value)} style={inputStyle} onFocus={onInputFocus} onBlur={onInputBlur} />
          </FieldRow>

          <FieldRow label="Stall Timeout (seconds)" description="Seconds of no data before a torrent is considered stalled. Triggers auto-remediation.">
            <input type="number" min={30} value={getNum("stall_timeout", 120)} onChange={(e) => set("stall_timeout", e.target.value)} style={inputStyle} onFocus={onInputFocus} onBlur={onInputBlur} />
          </FieldRow>
        </>}>
          <FieldRow label="Max Active Downloads" description="0 = unlimited">
            <input type="number" min={0} value={getNum("max_active_downloads", 5)} onChange={(e) => set("max_active_downloads", e.target.value)} style={inputStyle} onFocus={onInputFocus} onBlur={onInputBlur} />
          </FieldRow>
        </SectionCard>
        </div>

        {/* Row 4: Hooks + Health */}
        <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 20, alignItems: "stretch" }}>
        {/* ── Hooks ──────────────────────────────────────────────────────── */}
        <SectionCard title="External Commands" icon={Terminal} advanced={<>
          <FieldRow label="Run on torrent added" description="Shell command executed when a torrent is added. Variables: %h (hash), %n (name), %c (category).">
            <input value={getStr("on_add_command")} onChange={(e) => set("on_add_command", e.target.value)} placeholder="e.g. /path/to/script.sh %h %n" style={{ ...inputStyle, fontFamily: "var(--font-family-mono)", fontSize: 12 }} onFocus={onInputFocus} onBlur={onInputBlur} />
          </FieldRow>

          <FieldRow label="Run on torrent completed" description="Shell command executed when a torrent finishes downloading. Variables: %h (hash), %n (name), %p (path), %c (category).">
            <input value={getStr("on_complete_command")} onChange={(e) => set("on_complete_command", e.target.value)} placeholder="e.g. /path/to/import.sh %p" style={{ ...inputStyle, fontFamily: "var(--font-family-mono)", fontSize: 12 }} onFocus={onInputFocus} onBlur={onInputBlur} />
          </FieldRow>
        </>}>
          <p style={{ margin: 0, fontSize: 12, color: "var(--color-text-muted)" }}>
            Run shell commands when torrents are added or completed. Useful for post-processing scripts.
          </p>
        </SectionCard>

        {/* ── Engine Health ───────────────────────────────────────────────── */}
        {health && (
          <SectionCard title="Engine Health" icon={Activity}>
            <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(120px, 1fr))", gap: 12 }}>
              {[
                { label: "Status", value: health.engine_status, color: health.engine_status === "healthy" ? "var(--color-success)" : "var(--color-danger)" },
                { label: "VPN", value: health.vpn_active ? "Protected" : "Not Active", color: health.vpn_active ? "var(--color-success)" : "var(--color-danger)" },
                { label: "External IP", value: health.external_ip || "Unknown" },
                { label: "Peers", value: String(health.peers_connected) },
                { label: "Stalled", value: String(health.stalled_count), color: health.stalled_count > 0 ? "var(--color-warning)" : undefined },
              ].map(({ label, value, color }) => (
                <div key={label} style={{ background: "var(--color-bg-elevated)", borderRadius: 6, padding: "10px 14px" }}>
                  <div style={{ fontSize: 10, fontWeight: 600, textTransform: "uppercase", letterSpacing: "0.06em", color: "var(--color-text-muted)", marginBottom: 2 }}>{label}</div>
                  <div style={{ fontSize: 14, fontWeight: 500, color: color || "var(--color-text-primary)", textTransform: "capitalize" }}>{value}</div>
                </div>
              ))}
            </div>

            {/* Disk usage bar */}
            <div>
              <div style={{ display: "flex", justifyContent: "space-between", marginBottom: 6 }}>
                <span style={{ fontSize: 12, color: "var(--color-text-muted)" }}>
                  {formatBytes(health.disk_total_bytes - health.disk_free_bytes)} used of {formatBytes(health.disk_total_bytes)}
                </span>
                <span style={{ fontSize: 12, color: "var(--color-text-secondary)", fontWeight: 500 }}>{diskPercent.toFixed(1)}%</span>
              </div>
              <div style={{ height: 6, borderRadius: 3, background: "var(--color-bg-subtle)" }}>
                <div style={{
                  width: `${Math.min(diskPercent, 100)}%`,
                  height: "100%",
                  borderRadius: 3,
                  background: diskPercent > 90 ? "var(--color-danger)" : diskPercent > 75 ? "var(--color-warning)" : "var(--color-accent)",
                }} />
              </div>
              <div style={{ fontSize: 11, color: "var(--color-text-muted)", marginTop: 4 }}>{formatBytes(health.disk_free_bytes)} free</div>
            </div>
          </SectionCard>
        )}
        </div>

        {/* Row 5: About (full width) */}
        {/* ── About ──────────────────────────────────────────────────────── */}
        <SectionCard title="About" icon={Anchor}>
          <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
            {[
              { label: "Version", value: stats?.version ?? "-", mono: true },
              { label: "Total Torrents", value: String(stats?.total_torrents ?? 0) },
              { label: "API Docs", value: "/api/docs", link: true },
              { label: "OpenAPI Spec", value: "/api/openapi", link: true },
            ].map(({ label, value, mono, link }) => (
              <div key={label} style={{ display: "flex", justifyContent: "space-between", padding: "6px 0", borderBottom: "1px solid var(--color-border-subtle)" }}>
                <span style={{ fontSize: 13, color: "var(--color-text-secondary)" }}>{label}</span>
                {link ? (
                  <a href={value} target="_blank" rel="noopener" style={{ fontSize: 13, color: "var(--color-accent)", textDecoration: "none" }}>{value}</a>
                ) : (
                  <span style={{ fontSize: 13, color: "var(--color-text-primary)", fontFamily: mono ? "var(--font-family-mono)" : undefined }}>{value}</span>
                )}
              </div>
            ))}
          </div>

          <p style={{ margin: 0, fontSize: 12, color: "var(--color-text-muted)" }}>
            Startup settings are configured via config.yaml or HAUL_* environment variables.
            Runtime settings changed here are persisted in the database and take effect immediately.
          </p>
        </SectionCard>

      </div>
    </div>
  );
}
