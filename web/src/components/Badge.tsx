import type { ReactNode } from "react";

/* Status is never communicated by colour alone: every badge pairs its
 * colour with a glyph and a written label. Two of the four status colours
 * fall below 3:1 on the light surface by design, and this pairing is the
 * mitigation. */

export type Tone = "good" | "warning" | "serious" | "critical" | "neutral";

const TONE: Record<Tone, { fg: string; bg: string; ring: string }> = {
  good: { fg: "#065f06", bg: "#e6f5e6", ring: "#0ca30c" },
  warning: { fg: "#6b4a00", bg: "#fdf3dd", ring: "#fab219" },
  serious: { fg: "#7d3517", bg: "#fdeae1", ring: "#ec835a" },
  critical: { fg: "#7a1f1f", bg: "#fae7e7", ring: "#d03b3b" },
  neutral: { fg: "var(--color-ink-muted)", bg: "var(--color-surface-sunken)", ring: "var(--color-line-strong)" },
};

const GLYPH: Record<Tone, string> = {
  good: "M3.5 8.5l3 3 6-7",
  warning: "M8 4.5v4.5M8 11.4v.2",
  serious: "M8 4.5v4.5M8 11.4v.2",
  critical: "M4.5 4.5l7 7M11.5 4.5l-7 7",
  neutral: "M4.5 8h7",
};

export function Badge({
  tone,
  label,
  title,
}: {
  tone: Tone;
  label: string;
  title?: string;
}) {
  const t = TONE[tone];
  return (
    <span
      title={title ?? label}
      className="inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-xs font-medium whitespace-nowrap"
      style={{ color: t.fg, background: t.bg, boxShadow: `inset 0 0 0 1px ${t.ring}` }}
    >
      <svg width="11" height="11" viewBox="0 0 16 16" aria-hidden="true" className="shrink-0">
        <path d={GLYPH[tone]} stroke="currentColor" strokeWidth="2" strokeLinecap="round" fill="none" />
      </svg>
      {label}
    </span>
  );
}

/** Severity of a security alert. */
export function severityTone(sev: string): Tone {
  switch (sev.toLowerCase()) {
    case "critical":
      return "critical";
    case "high":
      return "serious";
    case "medium":
      return "warning";
    default:
      return "neutral";
  }
}

export function Pill({ children, title }: { children: ReactNode; title?: string }) {
  return (
    <span
      title={title}
      className="inline-flex items-center rounded px-1.5 py-0.5 text-xs whitespace-nowrap"
      style={{
        color: "var(--color-ink-muted)",
        background: "var(--color-surface-sunken)",
        boxShadow: "inset 0 0 0 1px var(--color-line)",
      }}
    >
      {children}
    </span>
  );
}
