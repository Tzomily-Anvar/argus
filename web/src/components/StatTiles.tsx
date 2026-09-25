/* Stat tiles, not a chart: these are single headline numbers, and a bar
 * chart of five unrelated counts would add ink without adding meaning.
 *
 * The number is the mark. Colour is applied sparingly and only where it
 * means something - a zero is always muted, because "nothing waiting on
 * you" should read as calm rather than as an alert coloured red. */

export type Tone = "critical" | "warning" | "good" | "neutral";

export type Stat = {
  label: string;
  value: number | string;
  hint: string;
  tone?: Tone;
  onClick?: () => void;
};

const COLOR: Record<Tone, string> = {
  critical: "var(--crit)",
  warning: "var(--warn)",
  good: "var(--good)",
  neutral: "var(--ink)",
};

export function StatTiles({ stats }: { stats: Stat[] }) {
  return (
    <div className="stat-grid">
      {stats.map((s) => (
        <button
          key={s.label}
          onClick={s.onClick}
          disabled={!s.onClick}
          title={s.hint}
          className="flex flex-col gap-1 px-4 py-3.5 text-left transition-colors enabled:cursor-pointer"
          style={{ background: "var(--surface)" }}
          onMouseEnter={(e) => (e.currentTarget.style.background = "var(--surface-2)")}
          onMouseLeave={(e) => (e.currentTarget.style.background = "var(--surface)")}
        >
          <span className="text-[11px] font-semibold tracking-wide uppercase" style={{ color: "var(--faint)" }}>
            {s.label}
          </span>
          <span
            className="tnum text-[28px] leading-none font-semibold"
            style={{ color: s.value === 0 ? "var(--faint)" : COLOR[s.tone ?? "neutral"] }}
          >
            {s.value}
          </span>
          <span className="text-[11.5px]" style={{ color: "var(--muted)" }}>
            {s.hint}
          </span>
        </button>
      ))}
    </div>
  );
}
