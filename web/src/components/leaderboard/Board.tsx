import type { Leaderboard, LeaderboardPeriod, Reviewer } from "../../api";
import { Badge } from "../Badge";
import { formatDateRange } from "../../dates";

/* The board itself: a podium for the top three and a table for everyone
 * else, over whichever period the page handed it. The page owns the
 * choice of period; this file only draws what it is given.
 *
 * It is meant to be fair as well as fun: a review of your own pull
 * request does not count, bots do not count, and equal figures share a
 * step. The arrow beside a name is movement against the comparison
 * period, not against anybody in particular. */

export function Board({ board, period, showTrend, title }: { board: Leaderboard; period: LeaderboardPeriod; showTrend: boolean; title?: string }) {
  const rows = period.rows ?? [];
  const podium = rows.filter((r) => r.rank <= 3);
  const rest = rows.filter((r) => r.rank > 3);
  const range = formatDateRange(period.from, period.to, true);

  return (
    <>
      <p className="mb-1 text-[13px]" style={{ color: "var(--muted)" }}>
        {title && `${title} · `}{range} · {period.reviews} {period.reviews === 1 ? "review" : "reviews"} on {period.prs} pull {period.prs === 1 ? "request" : "requests"}
        {board.truncated && " · the search hit GitHub's ceiling, so these are floors"}
      </p>
      {period.partial && (
        <p className="mb-1 text-[12px]" style={{ color: "var(--faint)" }}>
          This period starts before the sweep's {board.lookback_days}-day lookback, so its earliest reviews are not counted.
        </p>
      )}
      <div className="mb-3" />

      {rows.length === 0 ? (
        <div className="card px-6 py-12 text-center">
          <p className="text-2xl">🦗</p>
          <p className="mt-2 text-sm" style={{ color: "var(--muted)" }}>Nobody reviewed anything in this period. Somebody has to go first.</p>
        </div>
      ) : (
        <>
          <Podium rows={podium} showTrend={showTrend} />
          {rest.length > 0 && <Table rows={rest} showTrend={showTrend} />}
        </>
      )}
    </>
  );
}

const MEDAL: Record<number, string> = { 1: "🥇", 2: "🥈", 3: "🥉" };

/** The top three, the winner in the middle and taller, as podiums are. */
function Podium({ rows, showTrend }: { rows: Reviewer[]; showTrend: boolean }) {
  // Visual order second, first, third; with ties the extra medallists
  // simply line up after.
  const order = [...rows].sort((a, b) => a.rank - b.rank);
  const arranged = order.length >= 3 ? [order[1], order[0], order[2], ...order.slice(3)] : order;
  // The cards sit on a shared baseline, so more top padding is a taller
  // step: gold highest, bronze lowest.
  const heights: Record<number, string> = { 1: "pt-16", 2: "pt-10", 3: "pt-5" };
  return (
    <div className="mb-6 grid gap-3" style={{ gridTemplateColumns: `repeat(${Math.max(arranged.length, 1)}, minmax(0, 1fr))`, alignItems: "end" }}>
      {arranged.map((r) => (
        <div
          key={r.login}
          className={`card flex flex-col items-center px-4 pb-4 ${heights[r.rank] ?? "pt-5"} text-center`}
          style={r.rank === 1 ? { border: "1px solid var(--accent)" } : undefined}
        >
          <span className="text-3xl" aria-hidden="true">{MEDAL[r.rank]}</span>
          <Avatar login={r.login} rank={r.rank} />
          <a href={`https://github.com/${r.login}`} target="_blank" rel="noreferrer" className="mt-2 font-mono text-[13px] hover:underline" style={{ color: "var(--ink)" }}>
            {r.login}
          </a>
          <span className="tnum mt-1 text-2xl font-semibold">{r.reviews}</span>
          <span className="text-[12px]" style={{ color: "var(--muted)" }}>
            {r.reviews === 1 ? "review" : "reviews"} on {r.prs} {r.prs === 1 ? "PR" : "PRs"}
          </span>
          <span className="mt-2 flex flex-wrap items-center justify-center gap-1">
            {r.approvals > 0 && <Badge tone="good" label={`${r.approvals} approved`} />}
            {r.changes_requested > 0 && <Badge tone="warning" label={`${r.changes_requested} changes`} />}
            {r.comments > 0 && <Badge tone="neutral" label={`${r.comments} comments`} />}
            {showTrend && <Trend r={r} />}
          </span>
        </div>
      ))}
    </div>
  );
}

function Table({ rows, showTrend }: { rows: Reviewer[]; showTrend: boolean }) {
  return (
    <div className="card overflow-x-auto">
      <table className="w-full border-collapse text-[13px]">
        <thead style={{ background: "var(--surface-2)" }}>
          <tr>
            {["#", "Reviewer", "Reviews", "PRs", "Approved", "Changes", "Comments", showTrend ? "vs last" : ""].map((h) => (
              <th key={h} className="px-4 py-2 text-left text-[11px] font-semibold tracking-wide uppercase" style={{ color: "var(--faint)" }}>{h}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((r) => (
            <tr key={r.login} style={{ borderTop: "1px solid var(--line)" }}>
              <td className="tnum px-4 py-2.5" style={{ color: "var(--muted)" }}>{r.rank}</td>
              <td className="px-4 py-2.5">
                <span className="flex items-center gap-2">
                  <Avatar login={r.login} rank={r.rank} small />
                  <a href={`https://github.com/${r.login}`} target="_blank" rel="noreferrer" className="font-mono hover:underline" style={{ color: "var(--ink)" }}>{r.login}</a>
                </span>
              </td>
              <td className="tnum px-4 py-2.5 font-semibold">{r.reviews}</td>
              <td className="tnum px-4 py-2.5">{r.prs}</td>
              <td className="tnum px-4 py-2.5">{r.approvals}</td>
              <td className="tnum px-4 py-2.5">{r.changes_requested}</td>
              <td className="tnum px-4 py-2.5">{r.comments}</td>
              <td className="px-4 py-2.5">{showTrend && <Trend r={r} />}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

/** Movement against the comparison period: rank first, reviews when the rank held. */
function Trend({ r }: { r: Reviewer }) {
  if (r.previous_rank === 0) return <Badge tone="info" label="new" title="Not on the board last period" />;
  if (r.previous_rank > r.rank) return <Badge tone="good" label={`▲ ${r.previous_rank - r.rank}`} title={`Up from ${ordinal(r.previous_rank)}`} />;
  if (r.previous_rank < r.rank) return <Badge tone="warning" label={`▼ ${r.rank - r.previous_rank}`} title={`Down from ${ordinal(r.previous_rank)}`} />;
  const d = r.reviews - r.previous_reviews;
  return <Badge tone="neutral" label={d === 0 ? "held" : d > 0 ? `held, +${d}` : `held, ${d}`} title={`Held ${ordinal(r.rank)}; ${r.previous_reviews} reviews last period`} />;
}

function ordinal(n: number) {
  const s = ["th", "st", "nd", "rd"], v = n % 100;
  return n + (s[(v - 20) % 10] ?? s[v] ?? s[0]);
}

/** Initials in a circle. No avatar images: the page then loads nothing
 *  from outside the machine, which is the dashboard's promise. */
function Avatar({ login, rank, small }: { login: string; rank: number; small?: boolean }) {
  const initials = login.replace(/[^a-z0-9]/gi, "").slice(0, 2).toUpperCase() || "?";
  const size = small ? "h-6 w-6 text-[10px]" : "h-12 w-12 text-base";
  const tone = rank === 1 ? "var(--accent)" : "var(--surface-2)";
  const ink = rank === 1 ? "var(--accent-ink)" : "var(--ink)";
  return (
    <span className={`inline-flex ${size} shrink-0 items-center justify-center rounded-full font-semibold`} style={{ background: tone, color: ink, border: "1px solid var(--line)" }} aria-hidden="true">
      {initials}
    </span>
  );
}
