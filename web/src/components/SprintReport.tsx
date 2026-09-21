import { useState } from "react";
import type { SprintReport as Report, SprintFlag } from "../api";
import { Badge } from "./Badge";
import { StatTiles, type Stat } from "./StatTiles";

function flagTone(kind: string): "critical" | "warning" | "info" {
  switch (kind) {
    case "done_unassigned":
      return "critical";
    case "done_no_estimate":
      return "warning";
    default:
      return "info";
  }
}

function Section({
  title,
  hint,
  children,
  right,
}: {
  title: string;
  hint?: string;
  children: React.ReactNode;
  right?: React.ReactNode;
}) {
  return (
    <section className="card mb-4 overflow-hidden">
      <header className="flex flex-wrap items-baseline justify-between gap-2 px-4 pt-3.5 pb-2">
        <div>
          <h2 className="text-[15px] font-semibold">{title}</h2>
          {hint && (
            <p className="mt-0.5 text-[13px]" style={{ color: "var(--muted)" }}>
              {hint}
            </p>
          )}
        </div>
        {right}
      </header>
      <div style={{ borderTop: "1px solid var(--line)" }}>{children}</div>
    </section>
  );
}

/* Delivery per epic, as bars rather than a pie.
 *
 * A pie with eight slices cannot be read: two epics on the same share are
 * indistinguishable by angle, and a zero-point epic has no area at all.
 * Sorted bars give exact comparison, room for the full epic name, and the
 * Run/Build split stays legible. */
function EpicBars({ report }: { report: Report }) {
  const max = Math.max(...report.epics.map((e) => e.points), 1);
  const byClass = report.epics.reduce<Record<string, number>>((acc, e) => {
    if (e.class) acc[e.class] = (acc[e.class] ?? 0) + e.points;
    return acc;
  }, {});
  const classTotal = Object.values(byClass).reduce((a, b) => a + b, 0);

  return (
    <div className="px-4 py-3">
      {classTotal > 0 && (
        <div className="mb-4">
          <div className="mb-1.5 flex gap-3 text-[12px]">
            {Object.entries(byClass).map(([name, pts]) => (
              <span key={name} style={{ color: "var(--muted)" }}>
                <span className="font-semibold" style={{ color: "var(--ink)" }}>
                  {name}
                </span>{" "}
                {pts.toFixed(1)} pts · {((pts / classTotal) * 100).toFixed(0)}%
              </span>
            ))}
          </div>
          <div className="flex h-2 overflow-hidden rounded-full" style={{ background: "var(--surface-2)" }}>
            {Object.entries(byClass).map(([name, pts], i) => (
              <div
                key={name}
                title={`${name}: ${pts.toFixed(1)} pts`}
                style={{
                  width: `${(pts / classTotal) * 100}%`,
                  background: i === 0 ? "var(--accent)" : "var(--info)",
                  marginRight: i === 0 ? 2 : 0,
                }}
              />
            ))}
          </div>
        </div>
      )}

      <div className="space-y-2">
        {report.epics.map((e) => (
          <div key={e.name} className="flex items-center gap-3">
            <div className="w-56 shrink-0 truncate text-[13px]" title={e.name}>
              {e.name}
            </div>
            <div className="h-5 flex-1 overflow-hidden rounded" style={{ background: "var(--surface-2)" }}>
              <div
                className="h-full rounded"
                style={{
                  width: `${(e.points / max) * 100}%`,
                  background: e.class === "Run" ? "var(--info)" : "var(--accent)",
                  minWidth: e.points > 0 ? 3 : 0,
                }}
              />
            </div>
            <div className="tnum w-24 shrink-0 text-right text-[12.5px]" style={{ color: "var(--muted)" }}>
              {e.points.toFixed(1)} · {(e.share * 100).toFixed(0)}%
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

function PeopleTable({ report }: { report: Report }) {
  const [open, setOpen] = useState<string | null>(null);
  return (
    <div className="overflow-x-auto">
      <table className="w-full border-collapse text-sm">
        <thead style={{ background: "var(--surface-2)" }}>
          <tr>
            {["Person", "Baseline", "Capacity", "Delivered", "Delta", "Away"].map((h, i) => (
              <th
                key={h}
                className={`px-4 py-2.5 text-[11px] font-semibold tracking-wide uppercase ${i === 0 ? "text-left" : "text-right"}`}
                style={{ color: "var(--faint)" }}
              >
                {h}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {report.people.map((p) => (
            <>
              <tr
                key={p.account_id}
                onClick={() => setOpen(open === p.account_id ? null : p.account_id)}
                className="cursor-pointer [&:not(:last-child)]:border-b"
                style={{ borderColor: "var(--line)" }}
              >
                <td className="px-4 py-2.5">
                  {p.name}
                  {!p.registered && (
                    <span className="ml-2">
                      <Badge tone="info" label="not on roster" />
                    </span>
                  )}
                </td>
                <td className="tnum px-4 py-2.5 text-right" style={{ color: "var(--muted)" }}>
                  {p.registered ? p.baseline.toFixed(1) : "—"}
                </td>
                <td className="tnum px-4 py-2.5 text-right" style={{ color: "var(--muted)" }}>
                  {p.registered ? p.capacity.toFixed(1) : "—"}
                </td>
                <td className="tnum px-4 py-2.5 text-right font-semibold">{p.delivered.toFixed(1)}</td>
                <td
                  className="tnum px-4 py-2.5 text-right"
                  style={{ color: !p.registered ? "var(--faint)" : p.delta < 0 ? "var(--warn)" : "var(--good)" }}
                >
                  {p.registered ? (p.delta > 0 ? "+" : "") + p.delta.toFixed(1) : "—"}
                </td>
                <td className="tnum px-4 py-2.5 text-right text-[12.5px]" style={{ color: "var(--muted)" }}>
                  {p.planned_days_off + p.unplanned_days_off > 0
                    ? `${p.planned_days_off}p / ${p.unplanned_days_off}u`
                    : "—"}
                </td>
              </tr>
              {open === p.account_id && (
                <tr key={p.account_id + "-rows"}>
                  <td colSpan={6} className="px-4 py-3" style={{ background: "var(--surface-2)" }}>
                    {p.rows.length === 0 ? (
                      <span className="text-[13px]" style={{ color: "var(--muted)" }}>
                        Nothing delivered this sprint.
                      </span>
                    ) : (
                      <ul className="space-y-1">
                        {p.rows.map((r) => (
                          <li key={r.key} className="flex items-baseline gap-2 text-[13px]">
                            <a href={r.url} target="_blank" rel="noreferrer" className="lnk font-mono text-xs">
                              {r.key}
                            </a>
                            <span className="truncate">{r.summary}</span>
                            <span className="tnum ml-auto shrink-0" style={{ color: "var(--muted)" }}>
                              {r.has_points ? r.points.toFixed(1) : "no estimate"}
                            </span>
                          </li>
                        ))}
                      </ul>
                    )}
                  </td>
                </tr>
              )}
            </>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function Flags({ flags }: { flags: SprintFlag[] }) {
  if (flags.length === 0) {
    return (
      <p className="px-4 py-6 text-center text-sm" style={{ color: "var(--faint)" }}>
        Nothing needs a second look.
      </p>
    );
  }
  return (
    <ul className="divide-y" style={{ borderColor: "var(--line)" }}>
      {flags.map((f, i) => (
        <li key={i} className="flex flex-wrap items-center gap-2 px-4 py-2.5 text-[13px]">
          <Badge tone={flagTone(f.kind)} label={f.kind.replace(/_/g, " ")} />
          <span className="flex-1">{f.message}</span>
          {f.url && (
            <a href={f.url} target="_blank" rel="noreferrer" className="lnk shrink-0 text-xs">
              open
            </a>
          )}
          {/* Verify the whole set in Jira: trust in a number comes from
              being able to check it. */}
          {f.jql && (
            <a href={f.jql} target="_blank" rel="noreferrer" className="lnk shrink-0 text-xs">
              see all in Jira →
            </a>
          )}
        </li>
      ))}
    </ul>
  );
}

export function SprintReportView({ report }: { report: Report }) {
  const s = report.summary;
  const storyPts = report.stories_concluded.reduce((a, b) => a + b.points, 0);

  const stats: Stat[] = [
    { label: "Delivered", value: Math.round(s.delivered_total * 10) / 10, hint: "tasks and bugs, done", tone: "good" },
    { label: "Capacity", value: Math.round(s.capacity_total * 10) / 10, hint: "baseline less time away" },
    { label: "Say / do", value: s.completed, hint: `of ${s.promised + s.injected} (${s.injected} injected)` },
    { label: "Stories done", value: report.stories_concluded.length, hint: `${storyPts.toFixed(1)} pts, reported separately` },
    { label: "Needs a look", value: report.flags.length, hint: "flags raised", tone: report.flags.length > 0 ? "warning" : "neutral" },
  ];

  return (
    <>
      <div className="card mb-5 overflow-hidden">
        <StatTiles stats={stats} />
      </div>

      <Section
        title="Where the sprint went"
        hint="Delivery per epic. Sorted bars rather than a pie: two epics on the same share are indistinguishable by angle."
      >
        <EpicBars report={report} />
      </Section>

      <Section
        title="Per person"
        hint="Click a row for the tickets behind the number. Container types are excluded, so this is the work itself rather than rollups above it."
        right={
          s.unattributed_points > 0 ? (
            <Badge tone="warning" label={`${s.unattributed_points.toFixed(1)} pts unassigned`} />
          ) : undefined
        }
      >
        <PeopleTable report={report} />
      </Section>

      <Section
        title="Stories concluded"
        hint="Containers that finished this sprint. Their points are a rollup of the work beneath them, so they are reported here rather than counted as anyone's capacity."
      >
        {report.stories_concluded.length === 0 ? (
          <p className="px-4 py-6 text-center text-sm" style={{ color: "var(--faint)" }}>
            None finished this sprint.
          </p>
        ) : (
          <ul className="divide-y" style={{ borderColor: "var(--line)" }}>
            {report.stories_concluded.map((st) => (
              <li key={st.key} className="flex items-baseline gap-2 px-4 py-2.5 text-[13px]">
                <a href={st.url} target="_blank" rel="noreferrer" className="lnk font-mono text-xs">
                  {st.key}
                </a>
                <span className="flex-1 truncate">{st.summary}</span>
                <span className="tnum shrink-0" style={{ color: "var(--muted)" }}>
                  {st.points.toFixed(1)} pts
                </span>
              </li>
            ))}
          </ul>
        )}
      </Section>

      <Section title="Needs a look" hint="Each one names a single fixable thing.">
        <Flags flags={report.flags} />
      </Section>

      <Section title="Carryover" hint="Open at the end of the sprint, going into the next.">
        {report.carryover.length === 0 ? (
          <p className="px-4 py-6 text-center text-sm" style={{ color: "var(--faint)" }}>
            Nothing carried over.
          </p>
        ) : (
          <ul className="divide-y" style={{ borderColor: "var(--line)" }}>
            {report.carryover.map((r) => (
              <li key={r.key} className="flex items-baseline gap-2 px-4 py-2.5 text-[13px]">
                <a href={r.url} target="_blank" rel="noreferrer" className="lnk font-mono text-xs">
                  {r.key}
                </a>
                <span className="flex-1 truncate">{r.summary}</span>
                <span className="shrink-0 text-xs" style={{ color: "var(--muted)" }}>
                  {r.status}
                </span>
              </li>
            ))}
          </ul>
        )}
      </Section>
    </>
  );
}
