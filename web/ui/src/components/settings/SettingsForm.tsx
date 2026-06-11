import { useEffect, useState } from "react";
import { toast } from "sonner";
import { useSettings, useSaveSettings, type SettingsMap } from "@/api/settings";

// Shared form machinery for the Settings and Media Management pages:
// one dirty-tracked form initialized from the saved settings map, typed
// getters, and the save mutation with its toasts.
export function useSettingsForm() {
  const { data: savedSettings } = useSettings();
  const saveSettings = useSaveSettings();
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

  return { dirty, saving: saveSettings.isPending, set, getBool, getNum, getStr, handleSave };
}

export function SaveButton({ dirty, saving, onClick }: { dirty: boolean; saving: boolean; onClick: () => void }) {
  return (
    <button
      className="settings-save"
      onClick={onClick}
      disabled={!dirty || saving}
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
    >
      {saving ? "Saving…" : "Save Changes"}
    </button>
  );
}

export function ToggleRow({ label, description, checked, onChange, disabled }: {
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

export function FieldRow({ label, description, children }: { label: string; description?: string; children: React.ReactNode }) {
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
