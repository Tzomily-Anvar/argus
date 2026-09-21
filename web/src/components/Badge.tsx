import type { ReactNode } from "react";

/* Status is never communicated by colour alone: every badge pairs its
 * colour with a glyph and a written label. The semantic colours are
 * shared across every theme preset and never change with it - red has to
 * mean the same thing whichever theme you picked. */

export type Tone = "good" | "warning" | "critical" | "info" | "neutral";

const TONE: Record<Tone, { fg: string; bg: string }> = {
  good: { fg: "var(--good)", bg: "var(--good-bg)" },
  warning: { fg: "var(--warn)", bg: "var(--warn-bg)" },
  critical: { fg: "var(--crit)", bg: "var(--crit-bg)" },
  info: { fg: "var(--info)", bg: "var(--info-bg)" },
  neutral: { fg: "var(--muted)", bg: "var(--surface-2)" },
};

const GLYPH: Record<Tone, string> = {
  good: "M3.5 8.5l3 3 6-7",
  warning: "M8 4.5v4.5M8 11.4v.2",
  critical: "M4.5 4.5l7 7M11.5 4.5l-7 7",
  info: "M8 7.5v5M8 4.6v.2",
  neutral: "M4.5 8h7",
};

export function Badge({ tone, label, title }: { tone: Tone; label: string; title?: string }) {
  const t = TONE[tone];
  return (
    <span
      title={title ?? label}
      className="inline-flex items-center gap-1 rounded-md px-1.5 py-0.5 text-[11.5px] font-medium whitespace-nowrap"
      style={{ color: t.fg, background: t.bg }}
    >
      <svg width="11" height="11" viewBox="0 0 16 16" aria-hidden="true" className="shrink-0">
        <path d={GLYPH[tone]} stroke="currentColor" strokeWidth="2" strokeLinecap="round" fill="none" />
      </svg>
      {label}
    </span>
  );
}

/** Severity of a security alert, mapped onto the shared status colours. */
export function severityTone(sev: string): Tone {
  switch (sev.toLowerCase()) {
    case "critical":
      return "critical";
    case "high":
      return "warning";
    case "medium":
      return "info";
    default:
      return "neutral";
  }
}

export function Pill({ children, title }: { children: ReactNode; title?: string }) {
  return (
    <span
      title={title}
      className="inline-flex items-center rounded-md px-1.5 py-0.5 text-[11.5px] whitespace-nowrap"
      style={{ color: "var(--muted)", background: "var(--surface-2)" }}
    >
      {children}
    </span>
  );
}
