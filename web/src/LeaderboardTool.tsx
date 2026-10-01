import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { fetchLeaderboard, fetchSnapshot, fetchSprints, fetchTools, type LeaderboardQuery, type SprintOption } from "./api";
import { Board } from "./components/leaderboard/Board";
import { ToolHeader } from "./components/ToolHeader";
import { Notice } from "./components/Notice";

/* The Leaderboard: who is reviewing.
 *
 * The sweep keeps every review it read over the last couple of months;
 * this page asks the server to count them over a period and the one
 * before it. Two ways to pick the period: Rolling, the fortnight ending
 * at the last sweep, and Sprint, a Jira sprint's own dates - offered
 * only when the sprint tool is live, because the sprint list comes from
 * it. Either way, the comparison period supplies the arrows.
 *
 * It is for fun. Review is teamwork, and the board is a thank-you to
 * the people doing it, not a target for anyone; the page says so under
 * the title so nobody mistakes it for a metric. */

type Mode = "rolling" | "sprint";
type Which = "current" | "previous";
const MODE_KEY = "argus-leaderboard-mode";

export function LeaderboardTool() {
  const snapshot = useQuery({ queryKey: ["snapshot"], queryFn: fetchSnapshot, refetchInterval: 60_000 });
  const tools = useQuery({ queryKey: ["tools"], queryFn: fetchTools, staleTime: Infinity });
  const sprintTool = tools.data?.tools.some((t) => t.id === "sprint" && t.available) ?? false;

  const [mode, setMode] = usePersistedMode();
  // Remembered as Sprint on a machine whose sprint tool has since gone
  // away: fall back rather than show an empty picker.
  const effectiveMode: Mode = mode === "sprint" && sprintTool ? "sprint" : "rolling";
  const [which, setWhich] = useState<Which>("current");

  const sprints = useQuery({
    queryKey: ["sprints"],
    queryFn: () => fetchSprints(false),
    staleTime: 60_000,
    enabled: effectiveMode === "sprint",
  });
  const dated = useMemo(() => (sprints.data?.sprints ?? []).filter(hasDates), [sprints.data]);
  const [sprintID, setSprintID] = useState<number | null>(null);
  // Default to the active sprint, or the latest closed one when nothing
  // is running.
  useEffect(() => {
    if (sprintID === null && dated.length) setSprintID(defaultSprint(dated).jira_id);
  }, [dated, sprintID]);
  const chosen = dated.find((s) => s.jira_id === sprintID) ?? null;
  const before = chosen ? sprintBefore(dated, chosen) : null;

  const query: LeaderboardQuery | undefined =
    effectiveMode === "sprint" && chosen
      ? { from: chosen.starts!, to: chosen.ends!, ...(before ? { prev_from: before.starts!, prev_to: before.ends! } : {}) }
      : undefined;
  const board = useQuery({
    queryKey: ["leaderboard", effectiveMode, query ?? "rolling"],
    queryFn: () => fetchLeaderboard(query),
    refetchInterval: 60_000,
    enabled: effectiveMode === "rolling" || chosen !== null,
  });

  const result = snapshot.data?.results["review_leaderboard"];
  const data = board.data;
  const period = data?.[which];
  const labels: Record<Which, string> =
    effectiveMode === "sprint"
      ? { current: "This sprint", previous: "Sprint before" }
      : { current: `This ${spanWord(data?.days ?? 14)}`, previous: `Last ${spanWord(data?.days ?? 14)}` };
  const title = effectiveMode === "sprint" ? (which === "current" ? chosen?.name : before?.name) : undefined;

  return (
    <div className="mx-auto max-w-4xl px-6 pt-6 pb-20">
      <ToolHeader
        title="Who is reviewing"
        lede="For fun. Review is teamwork, and this board is a thank-you to the people doing it, not a target for anyone."
        subtitle="Code reviews per person, from every pull request in the organisation. Your own pull requests and bots do not count."
      />

      <div className="mb-4 flex flex-wrap items-center gap-3">
        {sprintTool && (
          <Toggle
            value={effectiveMode}
            options={[["rolling", "Rolling"], ["sprint", "Sprint"]]}
            onChange={(m) => { setMode(m); setWhich("current"); }}
          />
        )}
        {effectiveMode === "sprint" && (
          <select
            className="rounded-lg px-2.5 py-1 text-[13px]"
            style={{ background: "var(--surface)", color: "var(--ink)", border: "1px solid var(--line)" }}
            value={sprintID ?? ""}
            onChange={(e) => { setSprintID(Number(e.target.value)); setWhich("current"); }}
            aria-label="Sprint"
          >
            {dated.length === 0 && <option value="">{sprints.isLoading ? "Loading sprints…" : "No dated sprints"}</option>}
            {dated.map((s) => (
              <option key={s.jira_id} value={s.jira_id}>{s.name}{s.state === "active" ? " (active)" : ""}</option>
            ))}
          </select>
        )}
        <span className="ml-auto">
          {data && <Toggle value={which} options={[["previous", labels.previous], ["current", labels.current]]} onChange={setWhich} />}
        </span>
      </div>

      {result?.error && <Notice className="mb-4">{result.error}</Notice>}
      {!snapshot.data?.ready && !result && (
        <p className="text-sm" style={{ color: "var(--muted)" }}>First sweep running. The board appears when it lands.</p>
      )}
      {snapshot.data?.ready && !result && (
        <p className="text-sm" style={{ color: "var(--muted)" }}>
          The review_leaderboard rule is off. Set ARGUS_RULE_REVIEW_LEADERBOARD_ENABLED=true to turn it on.
        </p>
      )}
      {result && !result.error && board.error && <Notice className="mb-4">{String(board.error)}</Notice>}
      {effectiveMode === "sprint" && sprints.error && (
        <p className="mb-4 text-sm" style={{ color: "var(--muted)" }}>The sprint list could not be read: {String(sprints.error)}</p>
      )}

      {data && period && <Board board={data} period={period} showTrend={which === "current"} title={title} />}
    </div>
  );
}

function Toggle<T extends string>({ value, options, onChange }: { value: T; options: [T, string][]; onChange: (v: T) => void }) {
  return (
    <div className="flex items-center gap-1 rounded-lg p-0.5" style={{ background: "var(--surface-2)", border: "1px solid var(--line)" }}>
      {options.map(([v, label]) => (
        <button
          key={v}
          onClick={() => onChange(v)}
          className="rounded-md px-3 py-1 text-[13px] font-medium"
          style={value === v
            ? { background: "var(--surface)", color: "var(--ink)", boxShadow: "var(--shadow)" }
            : { color: "var(--muted)" }}
        >
          {label}
        </button>
      ))}
    </div>
  );
}

function spanWord(days: number) {
  return days === 14 ? "fortnight" : days === 7 ? "week" : days === 30 || days === 31 ? "month" : `${days} days`;
}

/** The mode survives a reload. Private browsing may refuse storage, in
 *  which case the choice simply lasts the session. */
function usePersistedMode() {
  const [mode, setMode] = useState<Mode>(() => {
    try {
      return localStorage.getItem(MODE_KEY) === "sprint" ? "sprint" : "rolling";
    } catch {
      return "rolling";
    }
  });
  const set = (m: Mode) => {
    try {
      localStorage.setItem(MODE_KEY, m);
    } catch { /* private browsing */ }
    setMode(m);
  };
  return [mode, set] as const;
}

/** A sprint can be counted only when Jira gave it both dates. A zero
 *  Go time serialises as year 1, which is no date either. */
function hasDates(s: SprintOption) {
  return !!s.starts && !!s.ends && !s.starts.startsWith("0001") && !s.ends.startsWith("0001");
}

function defaultSprint(list: SprintOption[]) {
  return list.find((s) => s.state === "active") ?? list.find((s) => s.current) ?? latestEnding(list)!;
}

function latestEnding(list: SprintOption[]) {
  return list.reduce<SprintOption | null>((best, s) => (!best || at(s.ends) > at(best.ends) ? s : best), null);
}

/** A date as a number, so two RFC 3339 strings with different offsets
 *  still compare as moments. */
function at(iso?: string) {
  return Date.parse(iso ?? "");
}

/** The sprint before: the latest one that ended by the time this one
 *  started. Judged by dates rather than list order, so a board whose
 *  numbering has gaps still compares with its real predecessor. */
function sprintBefore(list: SprintOption[], chosen: SprintOption) {
  return latestEnding(list.filter((s) => s.jira_id !== chosen.jira_id && at(s.ends) <= at(chosen.starts)));
}
