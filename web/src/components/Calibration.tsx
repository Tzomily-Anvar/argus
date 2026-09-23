import type { Calibration, CalibrationTrend, Divergence } from "../api";
import { Badge, Pill } from "./Badge";

/* Estimate against actual.
 *
 * A team records two numbers on a ticket: what refinement agreed it would
 * cost, and what it turned out to cost. The distance between them says
 * something about how the refinement is reading the work.
 *
 * The framing is deliberate throughout. Nothing here is called accuracy,
 * because that word implies a right answer somebody failed to hit;
 * refinement is calibrating, and a sprint that came in over is a sprint
 * that learned something. The two directions are kept apart rather than
 * folded into one magnitude - costing more and costing less need
 * different conversations - and neither is coloured as an alarm.
 *
 * There is deliberately no stat tile for this. A single variance
 * percentage beside delivery and capacity would be read as a score, and
 * it is only meaningful next to the coverage it was drawn from. */

/** A share as a signed percentage. The sign is the whole point, so a
 *  positive one is written out rather than left implied. */
function pct(v: number): string {
  const n = Math.round(v * 100);
  return `${n > 0 ? "+" : ""}${n}%`;
}

function points(v: number): string {
  return (Math.round(v * 10) / 10).toFixed(1).replace(/\.0$/, "");
}

/** Which way it went, in words, because a sign alone is easy to misread
 *  and "over" on its own sounds like a verdict. */
function direction(c: Calibration): string {
  if (c.difference > 0) return "more than refinement expected";
  if (c.difference < 0) return "less than refinement expected";
  return "level with refinement";
}

/* The run of sprints, as a bar each side of a centre line.
 *
 * Diverging rather than stacked because zero is the meaningful middle
 * here, not the bottom of a scale: a sprint level with its estimates
 * should have no bar at all, and the eye should be able to tell the two
 * directions apart without reading the numbers.
 *
 * One colour for both directions, on purpose. Colouring overruns red and
 * underruns green would say that estimating low is a failure and
 * estimating high is a success, and neither is true - both are the same
 * distance from a calibrated estimate. */
function Trend({ trend, current }: { trend: CalibrationTrend; current: number }) {
  const run = trend.sprints.filter((s) => s.calibration.has_variance);
  if (run.length < 2) {
    return (
      <p className="px-4 py-3 text-[13px]" style={{ color: "var(--faint)" }}>
        {trend.building
          ? "Working out the run across earlier sprints. It takes a sweep each, so it fills in as they land."
          : "Not enough swept sprints yet to draw a run. One sprint is a number; several are a trend."}
      </p>
    );
  }

  // Scaled to the data, with a floor so a settled run of small variances
  // is not blown up into dramatic bars.
  const cap = Math.max(0.25, ...run.map((s) => Math.abs(s.calibration.variance)));
  const anyThin = run.some((s) => s.calibration.thin);

  return (
    <div className="px-4 py-3">
      <div className="space-y-1.5">
        {run.map((s) => {
          const c = s.calibration;
          const width = (Math.abs(c.variance) / cap) * 50;
          const here = s.number === current;
          return (
            <div
              key={s.number}
              className="grid items-center gap-3 text-[12.5px]"
              style={{ gridTemplateColumns: "5rem 1fr 3.25rem 5.5rem" }}
            >
              <span className={here ? "font-semibold" : ""} style={{ color: here ? "var(--ink)" : "var(--muted)" }}>
                Sprint {s.number}
              </span>
              <div className="relative h-4 rounded" style={{ background: "var(--surface-2)" }}>
                <div
                  title={`${points(c.estimated)} estimated, ${points(c.actual)} actual`}
                  className="absolute rounded-sm"
                  style={{
                    top: 3,
                    bottom: 3,
                    left: c.variance > 0 ? "50%" : `${50 - width}%`,
                    width: `${width}%`,
                    minWidth: 2,
                    background: "var(--accent)",
                    // Paler where too few tickets could be compared for
                    // the figure to stand on its own.
                    opacity: c.thin ? 0.4 : 1,
                  }}
                />
                <div className="absolute inset-y-0" style={{ left: "50%", width: 1, background: "var(--line)" }} />
              </div>
              <span className="tnum text-right" style={{ color: here ? "var(--ink)" : "var(--muted)" }}>
                {pct(c.variance)}
              </span>
              <span className="tnum text-right text-[11.5px]" style={{ color: "var(--faint)" }}>
                {c.compared} of {c.finished}
              </span>
            </div>
          );
        })}
      </div>

      <div
        className="mt-2.5 grid gap-3 text-[11.5px]"
        style={{ gridTemplateColumns: "5rem 1fr 3.25rem 5.5rem", color: "var(--faint)" }}
      >
        <span />
        <span className="flex justify-between">
          <span>← cost less</span>
          <span>cost more →</span>
        </span>
        <span />
        <span className="text-right">compared</span>
      </div>

      {anyThin && (
        <p className="mt-2 text-[12px]" style={{ color: "var(--faint)" }}>
          Paler bars are sprints where fewer than ten tickets could be compared. One ticket a few
          points out moves that figure on its own, so read it as a hint rather than as a reading.
        </p>
      )}
      {trend.building && (
        <p className="mt-2 text-[12px]" style={{ color: "var(--faint)" }}>
          Still sweeping the earlier sprints, so the run is not complete yet.
        </p>
      )}
    </div>
  );
}

/* The tickets that moved furthest, both directions.
 *
 * Most tickets match their estimate exactly, so the variance above is
 * usually the work of a handful. A refinement conversation starts from a
 * specific piece of work rather than from an average, which is why these
 * are named and linked instead of counted. */
function Diverged({ rows }: { rows: Divergence[] }) {
  return (
    <ul className="divide-y" style={{ borderColor: "var(--line)" }}>
      {rows.map((d) => (
        <li key={d.key} className="flex items-baseline gap-2 px-4 py-2.5 text-[13px]">
          <a href={d.url} target="_blank" rel="noreferrer" className="lnk font-mono text-xs">
            {d.key}
          </a>
          <span className="flex-1 truncate">{d.summary}</span>
          <span className="tnum shrink-0" style={{ color: "var(--muted)" }}>
            {points(d.estimated)} → {points(d.actual)}
          </span>
          {/* Signed, and in points rather than as a ratio: the ratio is
              visible in the two figures beside it, and points are what
              the sprint actually felt. */}
          <span className="tnum w-10 shrink-0 text-right" style={{ color: "var(--ink)" }}>
            {d.difference > 0 ? "+" : ""}
            {points(d.difference)}
          </span>
        </li>
      ))}
    </ul>
  );
}

export function CalibrationView({
  calibration,
  trend,
  sprintNumber,
}: {
  calibration: Calibration;
  trend?: CalibrationTrend;
  sprintNumber: number;
}) {
  const c = calibration;

  /* Degrade to one honest line rather than to an empty chart. A team
     using only one of the two fields has nothing to compare, and being
     shown a blank panel would leave them guessing whether that is a bug. */
  if (!c.configured) {
    return (
      <p className="px-4 py-5 text-[13px]" style={{ color: "var(--muted)" }}>
        Only one size is recorded on a ticket here, so there is nothing to compare. Point
        ARGUS_JIRA_ESTIMATE_FIELD_NAME at the field refinement fills in, and this becomes the gap
        between what work was expected to cost and what it cost.
      </p>
    );
  }
  if (c.compared === 0) {
    return (
      <p className="px-4 py-5 text-[13px]" style={{ color: "var(--muted)" }}>
        {c.finished === 0
          ? "Nothing finished here yet, so there is nothing to compare."
          : `None of the ${c.finished} tickets finished here carries both an estimate and an actual, so there is nothing to compare. A ticket needs both numbers before the gap between them means anything.`}
      </p>
    );
  }

  const coverage = Math.round((c.compared / Math.max(c.finished, 1)) * 100);

  return (
    <>
      <div className="px-4 py-3.5">
        <div className="flex flex-wrap items-baseline gap-x-6 gap-y-2">
          <div>
            <span className="text-[11px] font-semibold tracking-wide uppercase" style={{ color: "var(--faint)" }}>
              Estimated
            </span>
            <div className="tnum text-[22px] leading-tight font-semibold">{points(c.estimated)}</div>
          </div>
          <div>
            <span className="text-[11px] font-semibold tracking-wide uppercase" style={{ color: "var(--faint)" }}>
              Actual
            </span>
            <div className="tnum text-[22px] leading-tight font-semibold">{points(c.actual)}</div>
          </div>
          <div>
            <span className="text-[11px] font-semibold tracking-wide uppercase" style={{ color: "var(--faint)" }}>
              Difference
            </span>
            {/* Not coloured. There is no right answer here to have been
                missed, so a red number would be an accusation rather than
                a reading. */}
            <div className="tnum text-[22px] leading-tight font-semibold">
              {c.difference > 0 ? "+" : ""}
              {points(c.difference)}
              {c.has_variance && (
                <span className="ml-1.5 text-[13px] font-normal" style={{ color: "var(--muted)" }}>
                  {pct(c.variance)} {direction(c)}
                </span>
              )}
            </div>
          </div>
        </div>

        {/* Coverage sits with the totals, never below the fold. The
            figures above speak for the tickets that carry both numbers,
            which on most sprints is a subset, and a total that quietly
            implied it covered everything would be the dishonest part. */}
        <p className="mt-3 text-[13px]" style={{ color: "var(--muted)" }}>
          Across the {c.compared} of {c.finished} tickets finished here that carry both figures, so
          these totals speak for about {coverage}% of the sprint rather than all of it. The rest
          record one number or neither.
        </p>

        <div className="mt-2.5 flex flex-wrap items-center gap-2">
          <Pill title="Estimate and actual agreed exactly. Most tickets land here, so the gap above is usually the work of a handful.">
            {c.matched} matched
          </Pill>
          <Pill title="Cost more than refinement expected.">{c.over} cost more</Pill>
          <Pill title="Cost less than refinement expected.">{c.under} cost less</Pill>
          {c.thin && (
            <Badge
              tone="info"
              label="small sample"
              title="Fewer than ten tickets could be compared. One ticket a few points out moves this figure on its own, so it is worth reading as a hint rather than as a reading on refinement."
            />
          )}
        </div>
      </div>

      <div style={{ borderTop: "1px solid var(--line)" }}>
        <h3 className="px-4 pt-3 text-[13px] font-semibold">Across sprints</h3>
        <p className="px-4 pt-0.5 text-[12.5px]" style={{ color: "var(--muted)" }}>
          One sprint is a number. The run is what shows whether refinement is settling.
        </p>
        {trend ? (
          <Trend trend={trend} current={sprintNumber} />
        ) : (
          <p className="px-4 py-3 text-[13px]" style={{ color: "var(--faint)" }}>
            Working out the run across sprints…
          </p>
        )}
      </div>

      {c.diverged && c.diverged.length > 0 && (
        <div style={{ borderTop: "1px solid var(--line)" }}>
          <h3 className="px-4 pt-3 pb-2 text-[13px] font-semibold">
            Furthest from the estimate
            <span className="ml-2 font-normal" style={{ color: "var(--muted)" }}>
              both directions, biggest first
            </span>
          </h3>
          <Diverged rows={c.diverged} />
        </div>
      )}
    </>
  );
}
