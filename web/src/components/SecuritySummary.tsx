import { Badge, severityTone } from "./Badge";
import type { SecurityData } from "./Security";

/* The overview version: counts and the worst repositories, not the full
 * breakdown. Ten criticals in your own code is arguably the single most
 * important fact on the page, so it does not belong behind a click. */
export function SecuritySummary({ data, onOpen }: { data: SecurityData; onOpen: () => void }) {
  const db = data.dependabot;
  const worst = (db?.crit_high_by_repo ?? []).slice(0, 4);

  return (
    <div className="space-y-3 px-4 py-3.5">
      {db?.error ? (
        <p className="text-sm" style={{ color: "var(--warn)" }}>
          {db.error}
        </p>
      ) : (
        <>
          {db?.counts && (
            <div className="flex flex-wrap gap-1.5">
              {["critical", "high", "medium", "low"]
                .filter((s) => db.counts?.[s])
                .map((s) => (
                  <Badge key={s} tone={severityTone(s)} label={`${db.counts?.[s]} ${s}`} />
                ))}
            </div>
          )}

          {worst.length > 0 ? (
            <ul className="space-y-1.5">
              {worst.map((r) => (
                <li key={r.repo} className="flex items-center justify-between gap-3 text-sm">
                  <span className="truncate font-mono text-xs">{r.repo}</span>
                  <span className="flex shrink-0 gap-1.5">
                    {r.critical > 0 && <Badge tone="critical" label={`${r.critical} critical`} />}
                    {r.high > 0 && <Badge tone="warning" label={`${r.high} high`} />}
                  </span>
                </li>
              ))}
            </ul>
          ) : (
            <p className="text-sm" style={{ color: "var(--muted)" }}>
              Nothing critical or high in your own code.
            </p>
          )}

          <button onClick={onOpen} className="lnk text-[13px] font-medium">
            Full breakdown →
          </button>
        </>
      )}
    </div>
  );
}
