/* A stat tile, not a chart: these are single headline numbers, and a bar
 * chart of five unrelated counts would add ink without adding meaning.
 * The number is the mark; colour is reserved for the ones that indicate
 * state. */

export type Stat = {
  label: string;
  value: number;
  hint: string;
  tone?: "critical" | "warning" | "neutral";
  onClick?: () => void;
};

export function StatTiles({ stats }: { stats: Stat[] }) {
  return (
    <div className="grid grid-cols-2 gap-px sm:grid-cols-3 lg:grid-cols-5"
         style={{ background: "var(--color-line)" }}>
      {stats.map((s) => (
        <button
          key={s.label}
          onClick={s.onClick}
          disabled={!s.onClick}
          title={s.hint}
          className="flex flex-col gap-1 p-4 text-left transition-colors enabled:cursor-pointer enabled:hover:brightness-95"
          style={{ background: "var(--color-surface-raised)" }}
        >
          <span className="text-xs font-medium tracking-wide uppercase"
                style={{ color: "var(--color-ink-faint)" }}>
            {s.label}
          </span>
          <span
            className="tnum text-3xl leading-none font-semibold"
            style={{
              color:
                s.value === 0
                  ? "var(--color-ink-faint)"
                  : s.tone === "critical"
                    ? "var(--color-status-critical)"
                    : s.tone === "warning"
                      ? "var(--color-ink)"
                      : "var(--color-ink)",
            }}
          >
            {s.value}
          </span>
          <span className="text-xs" style={{ color: "var(--color-ink-muted)" }}>
            {s.hint}
          </span>
        </button>
      ))}
    </div>
  );
}
