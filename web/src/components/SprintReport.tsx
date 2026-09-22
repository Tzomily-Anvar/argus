import { useState } from "react";
import type {
  SprintReport as Report,
  SprintFlag,
  EpicGroup,
  CapacityReview,
  CalibrationTrend,
} from "../api";
import { CalibrationView } from "./Calibration";
import { Badge } from "./Badge";
import { StatTiles, type Stat } from "./StatTiles";
import { formatDay } from "../dates";

function flagTone(kind: string): "critical" | "warning" | "info" {
  switch (kind) {
    case "done_unassigned":
      return "critical";
    case "done_no_estimate":
    case "no_baseline":
    case "story_work_unsized":
    case "story_points_mismatch":
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

/* An epic is a ticket, so it opens like one. Work that belongs to no
 * epic has no key to open, and neither has anything if no Jira base URL
 * is configured, so both read as the plain label they are. */
function EpicName({ epic }: { epic: EpicGroup }) {
  if (!epic.url) return <>{epic.name}</>;
  return (
    <a href={epic.url} target="_blank" rel="noreferrer" className="lnk">
      {epic.name}
    </a>
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
          <div key={e.key ?? e.name} className="flex items-center gap-3">
            <div className="w-56 shrink-0 truncate text-[13px]" title={e.key ? `${e.key} · ${e.name}` : e.name}>
              <EpicName epic={e} />
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
                  {/* Two different things, and conflating them is what
                      made this column confusing. Somebody nobody has
                      registered is a gap to close; somebody deliberately
                      left off the roster is an answer already given. Both
                      keep their delivered points either way. */}
                  {!p.on_roster && (
                    <span className="ml-2">
                      <Badge
                        tone="info"
                        label="not on roster"
                        title="Delivered work here without being on the team's roster. The points count towards the sprint; there is no baseline to measure them against."
                      />
                    </span>
                  )}
                  {p.on_roster && !p.measured && (
                    <span className="ml-2">
                      <Badge
                        tone="info"
                        label="not measured"
                        title="On the roster and opted out of measurement, so no baseline is compared. Their delivered points still count."
                      />
                    </span>
                  )}
                </td>
                <td className="tnum px-4 py-2.5 text-right" style={{ color: "var(--muted)" }}>
                  {p.measured ? p.baseline.toFixed(1) : "—"}
                </td>
                <td className="tnum px-4 py-2.5 text-right" style={{ color: "var(--muted)" }}>
                  {p.measured ? p.capacity.toFixed(1) : "—"}
                </td>
                <td className="tnum px-4 py-2.5 text-right font-semibold">{p.delivered.toFixed(1)}</td>
                <td
                  className="tnum px-4 py-2.5 text-right"
                  style={{ color: !p.measured ? "var(--faint)" : p.delta < 0 ? "var(--warn)" : "var(--good)" }}
                >
                  {p.measured ? (p.delta > 0 ? "+" : "") + p.delta.toFixed(1) : "—"}
                  {/* Unplanned absence no longer shrinks capacity, so a
                      shortfall stays visible as a shortfall. Saying how
                      much of it was absence nobody could plan around is
                      the explanation that used to be hidden inside the
                      capacity figure - it accounts for part of the gap,
                      it does not excuse it. */}
                  {p.measured && p.shortfall_from_absence > 0 && (
                    <div className="text-[11px] font-normal" style={{ color: "var(--faint)" }}>
                      {p.shortfall_from_absence.toFixed(1)} unplanned
                    </div>
                  )}
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
                            {/* Credited, not size. A ticket that spans
                                sprints is shared between them, so the
                                number in this table has to be the share
                                this sprint took or the rows will not add
                                up to the total above them. */}
                            <span className="tnum ml-auto shrink-0" style={{ color: "var(--muted)" }}>
                              {r.has_points
                                ? r.credited.toFixed(2).replace(/\.?0+$/, "") +
                                  (r.credited !== r.points ? ` of ${r.points.toFixed(1)}` : "")
                                : "no estimate"}
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

/* Whether these capacity figures have been confirmed by a person.
 *
 * Three states, not two. A sprint with nobody away and a sprint nobody
 * has filled in both have an empty capacity table, and reading the second
 * as the first would quietly turn a gap into a claim. */
function CapacityReviewBadge({ review }: { review: CapacityReview }) {
  const on = formatDay(review.reviewed_at);
  switch (review.state) {
    case "no_adjustments":
      return (
        <Badge
          tone="good"
          label="Capacity: nobody was away"
          title={`Checked on ${on}: everyone was at their baseline.`}
        />
      );
    case "adjusted":
      return (
        <Badge
          tone="good"
          label={`Capacity: ${review.adjusted} ${review.adjusted === 1 ? "person" : "people"} away`}
          title={`Checked on ${on}.`}
        />
      );
    default:
      return (
        <Badge
          tone="warning"
          label="Capacity not reviewed"
          title="Nobody has confirmed who was available, so these are baselines rather than measured capacity."
        />
      );
  }
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
          {/* A flag speaking for several tickets keeps them behind it, so
              one row can stand in for what would otherwise be nine. */}
          {f.keys && f.keys.length > 0 && (
            <span className="shrink-0 font-mono text-[11px]" style={{ color: "var(--faint)" }}>
              {f.keys.join(" ")}
            </span>
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

export function SprintReportView({ report, trend }: { report: Report; trend?: CalibrationTrend }) {
  const s = report.summary;
  const storyPts = report.stories_concluded.reduce((a, b) => a + b.points, 0);
  const carry = report.carryover;

  const stats: Stat[] = [
    {
      label: "Delivered",
      value: Math.round(s.delivered_total * 10) / 10,
      hint: s.finished_from_earlier > 0
        ? `tasks and bugs finished here · ${s.finished_from_earlier} started earlier`
        : "tasks and bugs finished here",
      tone: "good",
    },
    {
      label: "Capacity",
      value: Math.round(s.capacity_total * 10) / 10,
      hint: s.shortfall_from_absence > 0
        ? `baseline less planned leave · ${s.shortfall_from_absence.toFixed(1)} lost to unplanned`
        : "baseline less planned leave",
    },
    { label: "Say / do", value: s.completed, hint: `of ${s.promised + s.injected} (${s.injected} injected)` },
    { label: "Stories done", value: report.stories_concluded.length, hint: `${storyPts.toFixed(1)} pts, never in delivered` },
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
          <div className="flex flex-wrap items-center gap-2">
            <CapacityReviewBadge review={report.capacity_review} />
            {s.unattributed_points > 0 && (
              <Badge tone="warning" label={`${s.unattributed_points.toFixed(1)} pts unassigned`} />
            )}
          </div>
        }
      >
        <PeopleTable report={report} />
      </Section>

      <Section
        title="Stories concluded"
        hint="Finished here, meaning the Story is done and so is everything linked beneath it. Their points are a rollup of that work, so they are reported here and never counted as delivery."
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
                {/* The work underneath, beside the rollup above it. The
                    two disagreeing is the case worth looking at, and it
                    reads better here than as a flag nobody opens. */}
                <span className="tnum shrink-0 text-xs" style={{ color: "var(--faint)" }}>
                  {st.linked_count === 0
                    ? "no linked work"
                    : `${st.linked_count} linked · ${st.linked_points.toFixed(1)}`}
                </span>
              </li>
            ))}
          </ul>
        )}
      </Section>

      {/* Where this belongs, and why here.
       *
       * A section of the sprint report rather than a panel of its own.
       * The conversation it feeds is a retrospective, retrospectives are
       * held per sprint, and the run across sprints only means anything
       * beside the sprint somebody is reading - a separate panel would be
       * a second place to go for one conversation and would have to
       * re-ask which sprint anyway.
       *
       * Placed here, after what happened and before the housekeeping: it
       * is reflective material rather than a headline, and it is
       * deliberately not one of the stat tiles at the top, where a lone
       * variance percentage would be read as a score. */}
      <Section
        title="Estimate against actual"
        hint="Two numbers are recorded on a ticket: what refinement agreed it would cost, and what it turned out to cost. The distance between them is a reading on how refinement is calibrating, not a score."
      >
        <CalibrationView
          calibration={report.calibration}
          trend={trend}
          sprintNumber={report.sprint.number}
        />
      </Section>

      <Section title="Needs a look" hint="Each one names a single fixable thing.">
        <Flags flags={report.flags} />
      </Section>

      <Section
        title="Carryover"
        hint="Still open when the sprint closed, going into the next. Work nobody picked up is marked: it is not the same as work somebody could not finish."
        right={
          s.never_started > 0 ? (
            <Badge tone="info" label={`${s.never_started} never started`} />
          ) : undefined
        }
      >
        {carry.length === 0 ? (
          <p className="px-4 py-6 text-center text-sm" style={{ color: "var(--faint)" }}>
            Nothing carried over.
          </p>
        ) : (
          <ul className="divide-y" style={{ borderColor: "var(--line)" }}>
            {carry.map((r) => (
              <li key={r.key} className="flex items-baseline gap-2 px-4 py-2.5 text-[13px]">
                <a href={r.url} target="_blank" rel="noreferrer" className="lnk font-mono text-xs">
                  {r.key}
                </a>
                <span className="flex-1 truncate">{r.summary}</span>
                {!r.active && <Badge tone="info" label="never started" />}
                {r.active && !r.time_logged && <Badge tone="warning" label="no time logged" />}
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
