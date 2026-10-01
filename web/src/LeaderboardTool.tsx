import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { fetchSnapshot, type Leaderboard, type LeaderboardPeriod, type Reviewer } from "./api";
import { Badge } from "./components/Badge";
import { formatDateRange } from "./dates";

/* The Leaderboard: who is reviewing.
 *
 * A podium for the top three and a table for everyone else, over the
 * current fortnight, with last fortnight a click away. The figures come
 * from the pull request sweep's review_leaderboard rule, so this page
 * costs nothing extra and is as current as the dashboard beside it.
 *
 * It is meant to be fun, and it is meant to be fair: a review of your
 * own pull request does not count, bots do not count, and equal
 * figures share a step. The arrow beside a name is movement against
 * last period, not against anybody in particular. */

export function LeaderboardTool() {
  const snapshot = useQuery({ queryKey: ["snapshot"], queryFn: fetchSnapshot, refetchInterval: 60_000 });
  const [which, setWhich] = useState<"current" | "previous">("current");
  const result = snapshot.data?.results["review_leaderboard"];
  const board = result?.data as Leaderboard | undefined;
  const period = board?.[which];

  return (
    <div className="mx-auto max-w-4xl px-6 pt-6 pb-20">
      <header className="mb-5 flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-lg font-semibold tracking-tight">Who is reviewing</h1>
          <p className="mt-0.5 text-[13px]" style={{ color: "var(--muted)" }}>
            Code reviews per person, from every pull request in the organisation. Your own pull requests and bots do not count.
          </p>
        </div>
        {board && (
          <div className="flex items-center gap-1 rounded-lg p-0.5" style={{ background: "var(--surface-2)", border: "1px solid var(--line)" }}>
            {(["previous", "current"] as const).map((w) => (
              <button
                key={w}
                onClick={() => setWhich(w)}
                className="rounded-md px-3 py-1 text-[13px] font-medium"
                style={which === w
                  ? { background: "var(--surface)", color: "var(--ink)", boxShadow: "var(--shadow)" }
                  : { color: "var(--muted)" }}
              >
                {w === "current" ? `This ${spanWord(board.days)}` : `Last ${spanWord(board.days)}`}
              </button>
            ))}
          </div>
        )}
      </header>

      {result?.error && (
        <p className="mb-4 rounded-xl px-3.5 py-2.5 text-sm" style={{ background: "var(--crit-bg)", color: "var(--crit)" }}>{result.error}</p>
      )}
      {!snapshot.data?.ready && !result && (
        <p className="text-sm" style={{ color: "var(--muted)" }}>First sweep running. The board appears when it lands.</p>
      )}
      {snapshot.data?.ready && !result && (
        <p className="text-sm" style={{ color: "var(--muted)" }}>
          The review_leaderboard rule is off. Set ARGUS_RULE_REVIEW_LEADERBOARD_ENABLED=true to turn it on.
        </p>
      )}

      {board && period && <Board board={board} period={period} which={which} />}
    </div>
  );
}

function spanWord(days: number) {
  return days === 14 ? "fortnight" : days === 7 ? "week" : days === 30 || days === 31 ? "month" : `${days} days`;
}

function Board({ board, period, which }: { board: Leaderboard; period: LeaderboardPeriod; which: "current" | "previous" }) {
  const rows = period.rows ?? [];
  const podium = rows.filter((r) => r.rank <= 3);
  const rest = rows.filter((r) => r.rank > 3);
  const range = formatDateRange(period.from, period.to, true);

  return (
    <>
      <p className="mb-4 text-[13px]" style={{ color: "var(--muted)" }}>
        {range} · {period.reviews} {period.reviews === 1 ? "review" : "reviews"} on {period.prs} pull {period.prs === 1 ? "request" : "requests"}
        {board.truncated && " · the search hit GitHub's ceiling, so these are floors"}
      </p>

      {rows.length === 0 ? (
        <div className="card px-6 py-12 text-center">
          <p className="text-2xl">🦗</p>
          <p className="mt-2 text-sm" style={{ color: "var(--muted)" }}>Nobody reviewed anything in this period. Somebody has to go first.</p>
        </div>
      ) : (
        <>
          <Podium rows={podium} showTrend={which === "current"} />
          {rest.length > 0 && <Table rows={rest} showTrend={which === "current"} />}
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

/** Movement against last period: rank first, reviews when the rank held. */
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
