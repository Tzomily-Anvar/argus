import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  fetchRules, fetchSnapshot, requestRefresh,
  type BranchRow, type MergeReadiness, type PRRow, type RuleInfo,
} from "./api";
import { applyTheme, loadPrefs, savePrefs, type Prefs } from "./theme";
import { availableTools } from "./tools";
import { Sidebar } from "./components/Sidebar";
import { SectionTabs, type Section } from "./components/SectionTabs";
import { StatTiles, type Stat } from "./components/StatTiles";
import { BranchTable, Empty, PRTable } from "./components/Tables";
import { Security, type SecurityData } from "./components/Security";
import { RulesPanel } from "./components/RulesPanel";
import { PolicyHintBanner } from "./components/PolicyHint";
import { Overview, preview, type Block } from "./components/Overview";

function ago(seconds: number): string {
  if (seconds < 60) return "just now";
  const m = Math.floor(seconds / 60);
  if (m < 60) return `${m} min ago`;
  const h = Math.floor(m / 60);
  return h < 24 ? `${h}h ago` : `${Math.floor(h / 24)}d ago`;
}

function useThemePrefs() {
  const [prefs, setPrefs] = useState<Prefs>(loadPrefs);
  useEffect(() => {
    applyTheme(prefs);
    savePrefs(prefs);
  }, [prefs]);
  return [prefs, setPrefs] as const;
}

function usePersistedFlag(key: string, initial: boolean) {
  const [on, setOn] = useState(() => {
    try {
      const v = localStorage.getItem(key);
      return v === null ? initial : v === "1";
    } catch {
      return initial;
    }
  });
  const toggle = () =>
    setOn((v) => {
      try {
        localStorage.setItem(key, v ? "0" : "1");
      } catch { /* private browsing */ }
      return !v;
    });
  return [on, toggle] as const;
}

/* A section is one view within the open tool. Most map onto a rule, but
 * some are a filtered view of one - "Ready to merge" is the approved and
 * green subset of the full open list, which is a different question from
 * "what is open", and deserves its own place rather than making the
 * reader scan a status column for it. */
type SectionDef = {
  id: string;
  label: string;
  title: string;
  why: string;
  count: number;
  urgent?: boolean;
  body: () => React.ReactNode;
};

export default function App() {
  const qc = useQueryClient();
  const [prefs, setPrefs] = useThemePrefs();
  const [tool, setTool] = useState("pr");
  const [active, setActive] = useState("overview");
  const [collapsed, toggleRail] = usePersistedFlag("argus-rail-collapsed", false);

  const snapshot = useQuery({
    queryKey: ["snapshot"],
    queryFn: fetchSnapshot,
    // Brisk while a sweep runs so the page settles by itself; idle
    // otherwise, so a tab left open all day costs nothing.
    refetchInterval: (q) => (q.state.data?.sweeping ? 2_000 : 30_000),
  });
  const rules = useQuery({ queryKey: ["rules"], queryFn: fetchRules, staleTime: Infinity });
  const refresh = useMutation({
    mutationFn: requestRefresh,
    onSuccess: () => qc.invalidateQueries({ queryKey: ["snapshot"] }),
  });

  const data = snapshot.data;
  const ruleList: RuleInfo[] = rules.data?.rules ?? [];
  const why = (id: string) => ruleList.find((r) => r.id === id)?.why ?? "";

  const list = <T,>(id: string): T[] => {
    const r = data?.results[id];
    return !r || r.error ? [] : ((Array.isArray(r.data) ? r.data : []) as T[]);
  };

  const onYou = list<PRRow>("review_requested");
  const unclaimed = list<PRRow>("unreviewed");
  const mine = list<PRRow>("my_prs");
  const branches = list<BranchRow>("stale_branches");
  const stale = data?.results["stale_prs"]?.data as { rows?: PRRow[]; bot_count?: number } | undefined;
  const merge = data?.results["merge_readiness"]?.data as MergeReadiness | undefined;
  const allOpen = merge?.rows ?? [];
  const security = data?.results["security"]?.data as SecurityData | undefined;

  // Approved with nothing genuinely failing. Policy gates are excluded
  // from checks_green by the rule, so a missing label does not keep a
  // pull request out of this list.
  const ready = allOpen.filter((p) => p.review_decision === "APPROVED" && p.checks_green);

  const defs: SectionDef[] = [
    { id: "review_requested", label: "On you", title: "Waiting on your review", why: why("review_requested"),
      count: onYou.length, urgent: true, body: () => <PRTable rows={onYou} /> },
    { id: "ready", label: "Ready to merge", title: "Ready to merge",
      why: "Approved, with nothing actually failing. A missing policy label does not keep a pull request out of this list.",
      count: ready.length, body: () => <PRTable rows={ready} /> },
    { id: "unreviewed", label: "Unclaimed", title: "Nobody has picked these up", why: why("unreviewed"),
      count: unclaimed.length, body: () => <PRTable rows={unclaimed} /> },
    { id: "my_prs", label: "Yours", title: "Your open pull requests", why: why("my_prs"),
      count: mine.length, body: () => <PRTable rows={mine} showAuthor={false} /> },
    { id: "merge_readiness", label: "All open", title: "Open pull requests", why: why("merge_readiness"),
      count: allOpen.length, body: () => <PRTable rows={allOpen} /> },
    { id: "stale_prs", label: "Stale", title: "Stale pull requests", why: why("stale_prs"),
      count: stale?.rows?.length ?? 0, body: () => <PRTable rows={stale?.rows ?? []} /> },
    { id: "stale_branches", label: "Branches", title: "Stale branches", why: why("stale_branches"),
      count: branches.length, body: () => <BranchTable rows={branches} /> },
    { id: "security", label: "Security", title: "Security alerts", why: why("security"),
      count: security?.dependabot?.criticals?.length ?? 0, urgent: true,
      body: () => (security ? <Security data={security} /> : <Empty />) },
  ];

  const sections: Section[] = [
    { id: "overview", label: "Overview" },
    ...defs.map((d) => ({ id: d.id, label: d.label, count: d.count, urgent: d.urgent })),
    { id: "__rules", label: "Rules" },
  ];

  // The overview stacks the same sections, ordered by how much each one
  // is the reader's move. Security is excluded: it is a different shape
  // and a different question, and belongs in its own tab.
  const blocks: Block[] = defs
    .filter((d) => !["security", "merge_readiness"].includes(d.id))
    .map((d, i) => ({
      id: d.id,
      title: d.title,
      why: d.why,
      count: d.count,
      priority: 100 - i * 10,
      urgent: d.urgent,
      render:
        d.id === "stale_branches"
          ? preview.branches(branches)
          : preview.prs(
              d.id === "review_requested" ? onYou
              : d.id === "ready" ? ready
              : d.id === "unreviewed" ? unclaimed
              : d.id === "my_prs" ? mine
              : (stale?.rows ?? []),
              d.id !== "my_prs",
            ),
    }));

  const stats: Stat[] = useMemo(() => [
    { label: "On you", value: onYou.length, hint: "reviews requested from you", tone: "critical", onClick: () => setActive("review_requested") },
    { label: "Ready to merge", value: ready.length, hint: "approved and green", tone: "good", onClick: () => setActive("ready") },
    { label: "Unclaimed", value: unclaimed.length, hint: "no reviewer assigned", tone: "warning", onClick: () => setActive("unreviewed") },
    { label: "Stale", value: stale?.rows?.length ?? 0, hint: `${stale?.bot_count ?? 0} bot PRs not shown`, onClick: () => setActive("stale_prs") },
    { label: "Critical alerts", value: security?.dependabot?.criticals?.length ?? 0, hint: "first-party code only", tone: "critical", onClick: () => setActive("security") },
    // eslint-disable-next-line react-hooks/exhaustive-deps
  ], [data]);

  const def = defs.find((d) => d.id === active);
  const result = data?.results[active === "ready" ? "merge_readiness" : active];

  return (
    <div className={`shell${collapsed ? " collapsed" : ""}`}>
      <Sidebar
        tools={availableTools()}
        active={tool}
        onSelect={setTool}
        collapsed={collapsed}
        onToggle={toggleRail}
        user={data?.user}
        org={data?.org}
        prefs={prefs}
        onPrefs={setPrefs}
      />

      <main className="min-w-0">
        <div className="mx-auto max-w-5xl px-6 pt-6 pb-20">
          <header className="mb-5 flex flex-wrap items-center justify-between gap-3">
            <h1 className="text-lg font-semibold tracking-tight">Pull requests</h1>
            <div className="flex items-center gap-2.5">
              <span className="text-xs" style={{ color: "var(--faint)" }}>
                {data?.sweeping ? "sweeping…" : data?.ready ? `as of ${ago(data.age_seconds)}` : "first sweep…"}
              </span>
              <button
                onClick={() => refresh.mutate()}
                disabled={data?.sweeping}
                className="rounded-lg px-3 py-1.5 text-sm font-medium disabled:opacity-50"
                style={{ background: "var(--surface)", border: "1px solid var(--line)", boxShadow: "var(--shadow)" }}
              >
                Refresh
              </button>
            </div>
          </header>

          {data?.error && (
            <p className="mb-4 rounded-xl px-3.5 py-2.5 text-sm" style={{ background: "var(--crit-bg)", color: "var(--crit)" }}>
              {data.error}
            </p>
          )}

          {!data?.ready && !data?.error && (
            <p className="text-sm" style={{ color: "var(--muted)" }}>
              Running the first sweep. This takes a few seconds; afterwards the dashboard is served
              from memory and loads instantly.
            </p>
          )}

          {data?.ready && (
            <>
              <div className="card mb-6 overflow-hidden">
                <StatTiles stats={stats} />
              </div>

              <SectionTabs sections={sections} active={active} onSelect={setActive} />

              {active === "overview" ? (
                <Overview blocks={blocks} onOpen={setActive} />
              ) : active === "__rules" ? (
                <RulesPanel rules={ruleList} />
              ) : (
                <>
                  {def && (
                    <div className="mb-3">
                      <h2 className="text-[15px] font-semibold">{def.title}</h2>
                      <p className="mt-0.5 max-w-2xl text-sm" style={{ color: "var(--muted)" }}>
                        {def.why}
                      </p>
                    </div>
                  )}

                  {active === "merge_readiness" && merge?.policy_hints && (
                    <PolicyHintBanner hints={merge.policy_hints} />
                  )}

                  {result?.error ? (
                    <p className="rounded-xl px-3.5 py-2.5 text-sm" style={{ background: "var(--warn-bg)", color: "var(--warn)" }}>
                      {result.error}
                    </p>
                  ) : (
                    <div className="card overflow-hidden">{def ? def.body() : <Empty />}</div>
                  )}

                  {result && (
                    <p className="mt-2 text-xs" style={{ color: "var(--faint)" }}>
                      swept in {result.duration_ms} ms
                    </p>
                  )}
                </>
              )}
            </>
          )}
        </div>
      </main>
    </div>
  );
}
