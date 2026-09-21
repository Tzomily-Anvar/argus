import { useEffect, useRef, useState } from "react";
import { PRESETS, PRESET_ORDER, type Mode, type PresetKey, type Prefs } from "../theme";
import { Icon } from "./Icons";

export function ThemePicker({
  prefs,
  onChange,
  collapsed,
}: {
  prefs: Prefs;
  onChange: (p: Prefs) => void;
  collapsed: boolean;
}) {
  const [open, setOpen] = useState(false);
  const wrap = useRef<HTMLDivElement>(null);

  // Close on an outside click or Escape, so the popover never strands.
  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (wrap.current && !wrap.current.contains(e.target as Node)) setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && setOpen(false);
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  return (
    <div ref={wrap} className="relative" style={{ borderTop: "1px solid rgb(255 255 255 / 0.1)", marginTop: 8, paddingTop: 8 }}>
      <button className="navitem" onClick={() => setOpen((v) => !v)} aria-expanded={open} aria-label="Theme">
        <Icon.gear />
        <span className="navlabel">Theme</span>
      </button>

      {open && (
        <div
          className="absolute z-50 w-56 rounded-xl p-3.5"
          style={{
            left: collapsed ? "100%" : 0,
            bottom: collapsed ? 0 : "100%",
            marginLeft: collapsed ? 10 : 0,
            marginBottom: collapsed ? 0 : 10,
            background: "var(--surface)",
            color: "var(--ink)",
            border: "1px solid var(--line)",
            boxShadow: "var(--shadow)",
          }}
        >
          <h4 className="mb-2.5 text-[11.5px] font-bold tracking-wide uppercase" style={{ color: "var(--faint)" }}>
            Colour
          </h4>
          <div className="mb-3 grid grid-cols-2 gap-2">
            {PRESET_ORDER.map((key: PresetKey) => {
              const on = prefs.preset === key;
              return (
                <button
                  key={key}
                  onClick={() => onChange({ ...prefs, preset: key })}
                  className="flex items-center gap-1.5 rounded-lg px-2 py-1.5 text-left text-xs"
                  style={{
                    border: `1.5px solid ${on ? "var(--accent)" : "var(--line)"}`,
                    background: "var(--surface-2)",
                    color: on ? "var(--ink)" : "var(--muted)",
                    fontWeight: on ? 600 : 400,
                  }}
                >
                  <span
                    className="size-3 shrink-0 rounded-full"
                    style={{ background: PRESETS[key][prefs.mode].accent }}
                  />
                  {PRESETS[key].name}
                </button>
              );
            })}
          </div>

          <h4 className="mb-2.5 text-[11.5px] font-bold tracking-wide uppercase" style={{ color: "var(--faint)" }}>
            Mode
          </h4>
          <div className="flex gap-1.5">
            {(["light", "dark"] as Mode[]).map((m) => {
              const on = prefs.mode === m;
              return (
                <button
                  key={m}
                  onClick={() => onChange({ ...prefs, mode: m })}
                  className="flex flex-1 items-center justify-center gap-1.5 rounded-lg py-1.5 text-xs font-semibold capitalize"
                  style={{
                    border: `1.5px solid ${on ? "var(--accent)" : "var(--line)"}`,
                    background: "var(--surface-2)",
                    color: on ? "var(--ink)" : "var(--muted)",
                  }}
                >
                  {m === "light" ? <Icon.sun /> : <Icon.moon />}
                  {m}
                </button>
              );
            })}
          </div>
        </div>
      )}
    </div>
  );
}
