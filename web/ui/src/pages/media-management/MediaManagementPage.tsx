import { useState, useEffect } from "react";
import { Info } from "lucide-react";
import { useSettings, useSaveSettings, type SettingsMap } from "@/api/settings";
import { usePeerServices } from "@/api/activity";
import { toast } from "sonner";

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

function ToggleRow({ label, description, checked, onChange, disabled }: {
  label: string; description: string; checked: boolean; onChange: (v: boolean) => void; disabled?: boolean;
}) {
  return (
    <div style={{
      display: "flex",
      alignItems: "center",
      justifyContent: "space-between",
      gap: 16,
      paddingBottom: 16,
      borderBottom: "1px solid var(--color-border-subtle)",
      opacity: disabled ? 0.5 : 1,
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
        aria-disabled={disabled}
        disabled={disabled}
        onClick={() => !disabled && onChange(!checked)}
        style={{
          width: 40,
          height: 22,
          borderRadius: 11,
          border: "none",
          background: checked ? "var(--color-accent)" : "var(--color-bg-subtle)",
          cursor: disabled ? "not-allowed" : "pointer",
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

// ── Preview ───────────────────────────────────────────────────────────────

function FormatPreview({ format, type }: { format: string; type: "episode" | "movie" }) {
  const tokens: Record<string, string> = type === "episode"
    ? {
        "{Series Title}": "Breaking Bad",
        "{Series CleanTitle}": "Breaking Bad",
        "{Release Year}": "2008",
        "{Year}": "2008",
        "{Season:00}": "01",
        "{season:00}": "01",
        "{Episode:00}": "04",
        "{episode:00}": "04",
        "{Absolute Episode:000}": "004",
        "{Episode Title}": "Cancer Man",
        "{Quality Full}": "Bluray-1080p",
        "{MediaInfo VideoCodec}": "x265",
        "{Air Date}": "2008-02-17",
        "{Air-Date}": "2008-02-17",
        "{Original Title}": "Breaking Bad",
      }
    : {
        "{Movie Title}": "Fight Club",
        "{Movie CleanTitle}": "Fight Club",
        "{Release Year}": "1999",
        "{Year}": "1999",
        "{Quality Full}": "Remux-2160p",
        "{MediaInfo VideoCodec}": "x265",
      };

  let preview = format;
  for (const [token, value] of Object.entries(tokens)) {
    preview = preview.split(token).join(value);
  }

  return (
    <div style={{ fontSize: 11, color: "var(--color-text-muted)", marginTop: 4 }}>
      Preview: <span style={{ color: "var(--color-text-secondary)" }}>{preview}{type === "episode" ? ".mkv" : ".mkv"}</span>
    </div>
  );
}

// ── Page ──────────────────────────────────────────────────────────────────

export default function MediaManagementPage() {
  const { data: savedSettings } = useSettings();
  const saveSettings = useSaveSettings();
  const { data: peers } = usePeerServices();
  const [form, setForm] = useState<SettingsMap>({});
  const [dirty, setDirty] = useState(false);

  // When Pulse reports a Pilot or Prism peer, those services own the
  // rename step as part of their import pipeline. Letting Haul also
  // rename means the file's name changes underneath the arr's
  // importer, which then can't find it. Detect and disable.
  const pilotConnected = !!peers?.["pilot"];
  const prismConnected = !!peers?.["prism"];
  const arrConnected = pilotConnected || prismConnected;
  const arrNames = [pilotConnected && "Pilot", prismConnected && "Prism"].filter(Boolean).join(" and ");

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

  const episodeFormat = getStr("episode_format", "{Series Title} - S{Season:00}E{Episode:00} - {Episode Title} {Quality Full}");
  const movieFormat = getStr("movie_format", "{Movie Title} ({Release Year}) {Quality Full}");

  // The downstream Episode / Movie / Colon cards are only meaningful
  // when Haul will actually rename. With an arr connected, even if
  // the toggle is on (legacy setting) the rename never fires — so
  // grey the cards out the same way we do when the toggle is off.
  const renameActive = getBool("rename_on_complete") && !arrConnected;

  return (
    <div style={{ padding: 24, maxWidth: 800, margin: "0 auto" }}>
      {/* Header */}
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 24 }}>
        <div>
          <h1 style={{ fontSize: 20, fontWeight: 600, color: "var(--color-text-primary)", margin: 0 }}>Media Management</h1>
          <p style={{ fontSize: 13, color: "var(--color-text-secondary)", margin: "4px 0 0" }}>
            Configure how downloaded files are renamed
          </p>
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
          {saveSettings.isPending ? "Saving..." : "Save Changes"}
        </button>
      </div>

      <div style={{ display: "flex", flexDirection: "column", gap: 20 }}>
        {/* Master toggle */}
        <div style={{
          background: "var(--color-bg-surface)",
          border: "1px solid var(--color-border-subtle)",
          borderRadius: 10,
          padding: 20,
        }}>
          {arrConnected && (
            <div style={{
              display: "flex",
              gap: 10,
              padding: "10px 12px",
              marginBottom: 16,
              background: "color-mix(in srgb, var(--color-accent) 8%, transparent)",
              border: "1px solid color-mix(in srgb, var(--color-accent) 25%, transparent)",
              borderRadius: 6,
              fontSize: 12,
              color: "var(--color-text-secondary)",
              lineHeight: 1.5,
            }}>
              <Info size={14} style={{ color: "var(--color-accent)", flexShrink: 0, marginTop: 2 }} />
              <div>
                <strong style={{ color: "var(--color-text-primary)" }}>Renaming is handled by {arrNames}.</strong>
                {" "}When Haul is connected to a manager service, that service organises and renames imported files using its own format settings. Letting Haul also rename would change the filename out from under the importer mid-flight, so this setting is disabled.
                {" "}To configure naming, open {arrNames}'s Media Management settings.
              </div>
            </div>
          )}

          <ToggleRow
            label="Rename on complete"
            description="When a torrent finishes, rename its files using the formats below before the file is handed off. Only useful in standalone mode — Haul needs metadata (title, year, season/episode) to rename, and that metadata only arrives when a manager service stamps it on the torrent."
            checked={getBool("rename_on_complete") && !arrConnected}
            onChange={(v) => set("rename_on_complete", String(v))}
            disabled={arrConnected}
          />

          {!arrConnected && !getBool("rename_on_complete") && (
            <p style={{ margin: "8px 0 0", fontSize: 12, color: "var(--color-text-muted)" }}>
              When disabled, files keep their original torrent naming.
            </p>
          )}
          {!arrConnected && (
            <p style={{ margin: "10px 0 0", fontSize: 11, color: "var(--color-text-muted)", lineHeight: 1.5 }}>
              Tokens like <code style={{ fontFamily: "var(--font-family-mono)" }}>{"{Series Title}"}</code> and <code style={{ fontFamily: "var(--font-family-mono)" }}>{"{Quality Full}"}</code> are filled in from per-torrent metadata. Without that metadata (e.g. a magnet link added directly), the rename is a no-op and the file keeps its torrent name.
            </p>
          )}
        </div>

        {/* Episode Format */}
        <div style={{
          background: "var(--color-bg-surface)",
          border: "1px solid var(--color-border-subtle)",
          borderRadius: 10,
          overflow: "hidden",
          opacity: renameActive ? 1 : 0.5,
          pointerEvents: renameActive ? "auto" : "none",
        }}>
          <div style={{
            padding: "14px 20px",
            borderBottom: "1px solid var(--color-border-subtle)",
            background: "var(--color-bg-elevated)",
          }}>
            <span style={{ fontSize: 13, fontWeight: 600, color: "var(--color-accent)", letterSpacing: "0.01em" }}>
              TV Episodes
            </span>
          </div>
          <div style={{ padding: 20, display: "flex", flexDirection: "column", gap: 20 }}>
            <FieldRow label="Episode Format" description="Template for TV episode filenames. Tokens: {Series Title}, {Season:00}, {Episode:00}, {Episode Title}, {Quality Full}, {Air Date}">
              <input
                value={episodeFormat}
                onChange={(e) => set("episode_format", e.target.value)}
                style={{ ...inputStyle, fontFamily: "var(--font-family-mono)", fontSize: 12 }}
                onFocus={onInputFocus} onBlur={onInputBlur}
              />
              <FormatPreview format={episodeFormat} type="episode" />
            </FieldRow>

            <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 16 }}>
              <FieldRow label="Series Folder Format" description="Tokens: {Series Title}, {Release Year}">
                <input
                  value={getStr("series_folder_format", "{Series Title} ({Release Year})")}
                  onChange={(e) => set("series_folder_format", e.target.value)}
                  style={{ ...inputStyle, fontFamily: "var(--font-family-mono)", fontSize: 12 }}
                  onFocus={onInputFocus} onBlur={onInputBlur}
                />
              </FieldRow>
              <FieldRow label="Season Folder Format" description="Tokens: {Season:00}">
                <input
                  value={getStr("season_folder_format", "Season {Season:00}")}
                  onChange={(e) => set("season_folder_format", e.target.value)}
                  style={{ ...inputStyle, fontFamily: "var(--font-family-mono)", fontSize: 12 }}
                  onFocus={onInputFocus} onBlur={onInputBlur}
                />
              </FieldRow>
            </div>
          </div>
        </div>

        {/* Movie Format */}
        <div style={{
          background: "var(--color-bg-surface)",
          border: "1px solid var(--color-border-subtle)",
          borderRadius: 10,
          overflow: "hidden",
          opacity: renameActive ? 1 : 0.5,
          pointerEvents: renameActive ? "auto" : "none",
        }}>
          <div style={{
            padding: "14px 20px",
            borderBottom: "1px solid var(--color-border-subtle)",
            background: "var(--color-bg-elevated)",
          }}>
            <span style={{ fontSize: 13, fontWeight: 600, color: "var(--color-accent)", letterSpacing: "0.01em" }}>
              Movies
            </span>
          </div>
          <div style={{ padding: 20, display: "flex", flexDirection: "column", gap: 20 }}>
            <FieldRow label="Movie Format" description="Template for movie filenames. Tokens: {Movie Title}, {Release Year}, {Quality Full}">
              <input
                value={movieFormat}
                onChange={(e) => set("movie_format", e.target.value)}
                style={{ ...inputStyle, fontFamily: "var(--font-family-mono)", fontSize: 12 }}
                onFocus={onInputFocus} onBlur={onInputBlur}
              />
              <FormatPreview format={movieFormat} type="movie" />
            </FieldRow>

            <FieldRow label="Movie Folder Format" description="Tokens: {Movie Title}, {Release Year}">
              <input
                value={getStr("movie_folder_format", "{Movie Title} ({Release Year})")}
                onChange={(e) => set("movie_folder_format", e.target.value)}
                style={{ ...inputStyle, fontFamily: "var(--font-family-mono)", fontSize: 12 }}
                onFocus={onInputFocus} onBlur={onInputBlur}
              />
            </FieldRow>
          </div>
        </div>

        {/* Colon Replacement */}
        <div style={{
          background: "var(--color-bg-surface)",
          border: "1px solid var(--color-border-subtle)",
          borderRadius: 10,
          padding: 20,
          opacity: renameActive ? 1 : 0.5,
          pointerEvents: renameActive ? "auto" : "none",
        }}>
          <FieldRow label="Colon Replacement" description="How colons in titles are handled for filesystem compatibility.">
            <select value={getStr("colon_replacement", "space-dash")} onChange={(e) => set("colon_replacement", e.target.value)} style={{ ...inputStyle, fontFamily: "inherit" }} onFocus={onInputFocus} onBlur={onInputBlur}>
              <option value="space-dash">Space dash — "Title: Sub" becomes "Title - Sub"</option>
              <option value="dash">Dash — "Title: Sub" becomes "Title- Sub"</option>
              <option value="delete">Delete — "Title: Sub" becomes "Title Sub"</option>
            </select>
          </FieldRow>
        </div>
      </div>
    </div>
  );
}
