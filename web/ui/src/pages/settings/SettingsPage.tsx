import { useState } from "react";
import {
  Wifi, Download, Upload,
  Zap, Shield, Clock, Monitor, Moon, Sun, Check, Sparkles,
} from "lucide-react";
import { useSettingsForm, SaveButton, ToggleRow, FieldRow } from "@/components/settings/SettingsForm";
import {
  THEME_PRESETS,
  getStoredMode,
  getStoredPreset,
  resolveMode,
  setThemeMode,
  setThemePreset,
} from "@/theme";
import type { ThemeMode } from "@/theme";
import { setPieceAnimation, usePieceAnimation } from "@/lib/displayPrefs";
import Security from "@beacon-shared/Security";

// Appearance

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
            className="theme-pick-button"
            data-selected={selected}
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
                  className="theme-mode-button"
                  data-selected={active}
                  onClick={() => handleModeChange(m)}
                  style={{
                    display: "flex", alignItems: "center", gap: 6, padding: "6px 14px", borderRadius: 6,
                    border: active ? "1px solid var(--color-accent)" : "1px solid var(--color-border-default)",
                    background: active ? "var(--color-accent-muted)" : "var(--color-bg-elevated)",
                    color: active ? "var(--color-accent-hover)" : "var(--color-text-secondary)",
                    fontSize: 13, fontWeight: 500, cursor: "pointer",
                    transition: "background 120ms ease, border-color 120ms ease, color 120ms ease",
                  }}
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

// Reusable components

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
            className="linklike-button"
            onClick={() => setShowAdvanced((v) => !v)}
            style={{ alignSelf: "flex-start", fontSize: 11, display: "flex", alignItems: "center", gap: 4 }}
          >
            {showAdvanced ? "▾ Hide advanced" : "▸ Show advanced"}
          </button>
        )}
      </div>
    </div>
  );
}

// Disk-scale formatter: the dashboard cares about GB/TB, not KB.

// Animations: kept in this browser, like the theme, so it applies at once
// and needs no Save.
function AnimationSection() {
  const pieceAnimation = usePieceAnimation();
  return (
    <SectionCard title="Animations" icon={Sparkles}>
      <ToggleRow
        label="Animate the piece bar"
        description="On a torrent's page, show each piece dropping into place as it finishes. Off shows a plain bar that fills in. Saved in this browser."
        checked={pieceAnimation}
        onChange={setPieceAnimation}
      />
    </SectionCard>
  );
}

// Settings Page

export default function SettingsPage() {
  const { dirty, saving, set, getBool, getNum, getStr, handleSave } = useSettingsForm();


  return (
    <div style={{ padding: 24, maxWidth: 1100, margin: "0 auto" }}>
      {/* Header with save button */}
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 24 }}>
        <div>
          <h1 style={{ fontSize: 20, fontWeight: 600, color: "var(--color-text-primary)", margin: 0 }}>Settings</h1>
          <p style={{ fontSize: 13, color: "var(--color-text-secondary)", margin: "4px 0 0" }}>Configure Haul</p>
        </div>
        <SaveButton dirty={dirty} saving={saving} onClick={handleSave} />
      </div>

      <div style={{ display: "flex", flexDirection: "column", gap: 20 }}>

        {/* Appearance */}
        <AppearanceSection />

        {/* Animations */}
        <AnimationSection />

        {/* Row 1: Downloads + Speed */}
        <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 20, alignItems: "start" }}>
        {/* Downloads */}
        <SectionCard title="Downloads" icon={Download}>
          <FieldRow label="Default Save Path" description="Where new torrents save their data by default.">
            <input value={getStr("download_dir", "/home/Downloads")} onChange={(e) => set("download_dir", e.target.value)} className="settings-input" />
          </FieldRow>
        </SectionCard>

        {/* Speed */}
        <SectionCard title="Speed" icon={Zap} advanced={<>
          <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 16 }}>
            <FieldRow label="Alt Download Limit (KB/s)" description="Used during scheduled slow mode">
              <input type="number" min={0} value={getNum("alt_download_limit")} onChange={(e) => set("alt_download_limit", e.target.value)} className="settings-input" />
            </FieldRow>
            <FieldRow label="Alt Upload Limit (KB/s)" description="Used during scheduled slow mode">
              <input type="number" min={0} value={getNum("alt_upload_limit")} onChange={(e) => set("alt_upload_limit", e.target.value)} className="settings-input" />
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
                <input type="number" min={0} max={23} value={getNum("schedule_from_hour", 8)} onChange={(e) => set("schedule_from_hour", e.target.value)} className="settings-input" />
              </FieldRow>
              <FieldRow label="To (hour)">
                <input type="number" min={0} max={23} value={getNum("schedule_to_hour", 20)} onChange={(e) => set("schedule_to_hour", e.target.value)} className="settings-input" />
              </FieldRow>
              <FieldRow label="Days">
                <select value={getStr("schedule_days", "all")} onChange={(e) => set("schedule_days", e.target.value)} className="settings-input">
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
              <input type="number" min={0} value={getNum("global_download_limit")} onChange={(e) => set("global_download_limit", e.target.value)} className="settings-input" />
            </FieldRow>
            <FieldRow label="Global Upload Limit (KB/s)" description="0 = unlimited">
              <input type="number" min={0} value={getNum("global_upload_limit")} onChange={(e) => set("global_upload_limit", e.target.value)} className="settings-input" />
            </FieldRow>
          </div>
        </SectionCard>
        </div>

        {/* Row 2: BitTorrent + Seeding */}
        <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 20, alignItems: "start" }}>
        {/* BitTorrent */}
        <SectionCard title="BitTorrent" icon={Shield} advanced={<>
          <ToggleRow label="DHT" description="Distributed Hash Table for decentralized peer discovery." checked={getBool("enable_dht", true)} onChange={(v) => set("enable_dht", String(v))} />
          <ToggleRow label="PEX" description="Peer Exchange — learn about peers from other peers." checked={getBool("enable_pex", true)} onChange={(v) => set("enable_pex", String(v))} />
          <ToggleRow label="uTP" description="uTP transport protocol for better NAT traversal." checked={getBool("enable_utp", true)} onChange={(v) => set("enable_utp", String(v))} />
          <ToggleRow label="LSD" description="Local Service Discovery — find peers on your LAN." checked={getBool("enable_lsd", true)} onChange={(v) => set("enable_lsd", String(v))} />
        </>}>
          <FieldRow label="Encryption" description="Protocol encryption for peer connections.">
            <select value={getStr("encryption", "prefer")} onChange={(e) => set("encryption", e.target.value)} className="settings-input">
              <option value="prefer">Prefer — use encryption when available</option>
              <option value="require">Require — only connect with encryption</option>
              <option value="disable">Disable — never use encryption</option>
            </select>
          </FieldRow>
        </SectionCard>

        {/* Seeding */}
        <SectionCard title="Seeding" icon={Upload} advanced={<>
          <FieldRow label="Default Seed Time (minutes)" description="0 = unlimited. Stop seeding after this many minutes.">
            <input type="number" min={0} value={getNum("default_seed_time")} onChange={(e) => set("default_seed_time", e.target.value)} className="settings-input" />
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
                <input type="number" min={0} step={0.1} value={getNum("default_seed_ratio")} onChange={(e) => set("default_seed_ratio", e.target.value)} className="settings-input" />
              </FieldRow>
              <FieldRow label="Action on limit reached">
                <select value={getStr("seed_limit_action", "pause")} onChange={(e) => set("seed_limit_action", e.target.value)} className="settings-input">
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
        {/* Connection */}
        <SectionCard title="Connection" icon={Wifi} advanced={<>
          <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 16 }}>
            <FieldRow label="Max Global Connections" description="0 = unlimited">
              <input type="number" min={0} value={getNum("max_connections", 500)} onChange={(e) => set("max_connections", e.target.value)} className="settings-input" />
            </FieldRow>
            <FieldRow label="Max Per-Torrent Connections" description="0 = unlimited">
              <input type="number" min={0} value={getNum("max_connections_per_torrent", 100)} onChange={(e) => set("max_connections_per_torrent", e.target.value)} className="settings-input" />
            </FieldRow>
          </div>
        </>}>
          <FieldRow label="Listen Port" description="Port for incoming peer connections. Requires restart.">
            <input type="number" min={0} max={65535} value={getNum("listen_port", 6881)} onChange={(e) => set("listen_port", e.target.value)} className="settings-input" />
          </FieldRow>
        </SectionCard>

        {/* Queue */}
        <SectionCard title="Queue" icon={Clock} advanced={<>
          <FieldRow label="Stall Timeout (seconds)" description="Seconds of no data before a torrent is considered stalled. Triggers auto-remediation.">
            <input type="number" min={30} value={getNum("stall_timeout", 120)} onChange={(e) => set("stall_timeout", e.target.value)} className="settings-input" />
          </FieldRow>
        </>}>
          <FieldRow label="Max Active Downloads" description="0 = unlimited">
            <input type="number" min={0} value={getNum("max_active_downloads", 5)} onChange={(e) => set("max_active_downloads", e.target.value)} className="settings-input" />
          </FieldRow>
        </SectionCard>
        </div>


        <Security />



        <p style={{ margin: 0, fontSize: 12, color: "var(--color-text-muted)" }}>
            Startup settings are configured via config.yaml or HAUL_* environment variables.
            Runtime settings changed here are persisted in the database and take effect immediately.
          </p>

      </div>
    </div>
  );
}
