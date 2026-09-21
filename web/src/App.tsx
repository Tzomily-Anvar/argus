import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  fetchRules, fetchSnapshot, requestRefresh,
  type BranchRow, type MergeReadiness, type PRRow, type RuleInfo,
} from "./api";
import { applyTheme, loadPrefs, savePrefs, type Prefs } from "./theme";
import { Sidebar } from "./components/Sidebar";
import { SectionTabs, type Section } from "./components/SectionTabs";
import { availableTools } from "./tools";
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

export default function App() {
  const qc = useQueryClient();
  const [prefs, setPrefs] = useThemePrefs();
  const [tool, setTool] = useState("pr");
  const [active, setActive] = useState("overview");
  const [collapsed, setCollapsed] = useState(() => {
    try {
      return localStorage.getItem("argus-rail") === "collapsed";
    } catch {
      return false;
    }
  });

  const toggleRail = () => {
    setCollapsed((v) => {
      try {
        localStorage.setItem("argus-rail", v ? "open" : "collapsed");
      } catch { /* ignore */ }
      return !v;
    });
  };

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

  const rows = <T,>(id: string): T[] => {
    const r = data?.results[id];
    return !r || r.error ? [] : ((Array.isArray(r.data) ? r.data : []) as T[]);
  };

  const stale = data?.results["stale_prs"]?.data as { rows?: PRRow[]; bot_count?: number } | undefined;
  const merge = data?.results["merge_readiness"]?.data as MergeReadiness | undefined;
  const mergeRows = merge?.rows ?? [];
  const security = data?.results["security"]?.data as SecurityData | undefined;
  const mergeable = mergeRows.filter((p) => p.review_decision === "APPROVED" && p.checks_green);

  const sections: Section[] = [
    { id: "overview", label: "Overview" },
    { id: "review_requested", label: "On you", count: rows<PRRow>("review_requested").length, urgent: true },
    { id: "my_prs", label: "Yours", count: rows<PRRow>("my_prs").length },
    { id: "unreviewed", label: "Unclaimed", count: rows<PRRow>("unreviewed").length },
    { id: "merge_readiness", label: "All open", count: mergeRows.length },
    { id: "stale_prs", label: "Stale", count: stale?.rows?.length ?? 0 },
    { id: "stale_branches", label: "Branches", count: rows<BranchRow>("stale_branches").length },
    { id: "security", label: "Security", count: security?.dependabot?.criticals?.length ?? 0, urgent: true },
    { id: "__rules", label: "Rules" },
  ];

  const stats: Stat[] = useMemo(() => [
    { label: "On you", value: rows<PRRow>("review_requested").length, hint: "reviews requested from you", tone: "critical", onClick: () => setActive("review_requested") },
    { label: "Ready to merge", value: mergeable.length, hint: "approved and green", tone: "good", onClick: () => setActive("merge_readiness") },
    { label: "Unclaimed", value: rows<PRRow>("unreviewed").length, hint: "no reviewer assigned", tone: "warning", onClick: () => setActive("unreviewed") },
    { label: "Stale", value: stale?.rows?.length ?? 0, hint: `${stale?.bot_count ?? 0} bot PRs not shown`, onClick: () => setActive("stale_prs") },
    { label: "Critical alerts", value: security?.dependabot?.criticals?.length ?? 0, hint: "first-party code only", tone: "critical", onClick: () => setActive("security") },
    // eslint-disable-next-line react-hooks/exhaustive-deps
  ], [data]);

  const why = (id: string) => ruleList.find((r) => r.id === id)?.why ?? "";
  const blocks: Block[] = [
    { id: "review_requested", title: "Waiting on your review", why: why("review_requested"),
      count: rows<PRRow>("review_requested").length, priority: 100, urgent: true,
      render: preview.prs(rows<PRRow>("review_requested")) },
    { id: "merge_readiness", title: "Ready to merge", why: "Approved and green - merging these is the cheapest win available.",
      count: mergeable.length, priority: 90,
      render: preview.prs(mergeable) },
    { id: "unreviewed", title: "Nobody has picked these up", why: why("unreviewed"),
      count: rows<PRRow>("unreviewed").length, priority: 80,
      render: preview.prs(rows<PRRow>("unreviewed")) },
    { id: "my_prs", title: "Your open pull requests", why: why("my_prs"),
      count: rows<PRRow>("my_prs").length, priority: 70,
      render: preview.prs(rows<PRRow>("my_prs"), false) },
    { id: "stale_prs", title: "Stale pull requests", why: why("stale_prs"),
      count: stale?.rows?.length ?? 0, priority: 60,
      render: preview.prs(stale?.rows ?? []) },
    { id: "stale_branches", title: "Stale branches", why: why("stale_branches"),
      count: rows<BranchRow>("stale_branches").length, priority: 50,
      render: preview.branches(rows<BranchRow>("stale_branches")) },
  ];

  const activeRule = ruleList.find((r) => r.id === active);
  const activeResult = data?.results[active];

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
                  {activeRule && (
                    <div className="mb-3">
                      <h2 className="text-[15px] font-semibold">{activeRule.title}</h2>
                      <p className="mt-0.5 max-w-2xl text-sm" style={{ color: "var(--muted)" }}>
                        {activeRule.why}
                      </p>
                    </div>
                  )}

                  {active === "merge_readiness" && merge?.policy_hints && (
                    <PolicyHintBanner hints={merge.policy_hints} />
                  )}

                  {activeResult?.error ? (
                    <p className="rounded-xl px-3.5 py-2.5 text-sm" style={{ background: "var(--warn-bg)", color: "var(--warn)" }}>
                      {activeResult.error}
                    </p>
                  ) : (
                    <div className="card overflow-hidden">
                      {active === "security" && security ? (
                        <Security data={security} />
                      ) : active === "stale_branches" ? (
                        <BranchTable rows={rows<BranchRow>("stale_branches")} />
                      ) : active === "stale_prs" ? (
                        <PRTable rows={stale?.rows ?? []} />
                      ) : active === "merge_readiness" ? (
                        <PRTable rows={mergeRows} />
                      ) : active === "my_prs" ? (
                        <PRTable rows={rows<PRRow>("my_prs")} showAuthor={false} />
                      ) : ["review_requested", "unreviewed"].includes(active) ? (
                        <PRTable rows={rows<PRRow>(active)} />
                      ) : (
                        <Empty />
                      )}
                    </div>
                  )}

                  {activeResult && (
                    <p className="mt-2 text-xs" style={{ color: "var(--faint)" }}>
                      swept in {activeResult.duration_ms} ms
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
