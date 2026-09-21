import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  fetchRules, fetchSnapshot, requestRefresh,
  type BranchRow, type MergeReadiness, type PRRow, type RuleInfo,
} from "./api";
import { applyTheme, loadPrefs, savePrefs, type Prefs } from "./theme";
import { availableTools } from "./tools";
import { Icon } from "./components/Icons";
import { Sidebar } from "./components/Sidebar";
import { SectionTabs, type Section } from "./components/SectionTabs";
import { StatTiles, type Stat } from "./components/StatTiles";
import { Empty, PRTable } from "./components/Tables";
import { StaleView } from "./components/StaleView";
import { Security, type SecurityData } from "./components/Security";
import { SecuritySummary } from "./components/SecuritySummary";
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

  // Criticals alone would be the wrong measure of "is this clear?": an
  // organisation with three hundred highs and no criticals is not clear.
  // The overview asks about both, in first-party code.
  const critHigh = (security?.dependabot?.crit_high_by_repo ?? [])
    .reduce((n, r) => n + r.critical + r.high, 0);

  // Approved with nothing genuinely failing. Policy gates are excluded
  // from checks_green by the rule, so a red "missing label" check does
  // not by itself keep a pull request out of these lists.
  const approvedAndGreen = allOpen.filter((p) => p.review_decision === "APPROVED" && p.checks_green);

  // A QA label is optional, and most organisations do not use one. When
  // none is configured there is no QA stage to speak of, so the pile is
  // not split and the QA section never appears.
  const qaInUse = allOpen.some((p) => p.qa_label_used);
  const ready = qaInUse ? approvedAndGreen.filter((p) => p.has_qa_label) : approvedAndGreen;
  const awaitingQA = qaInUse ? approvedAndGreen.filter((p) => !p.has_qa_label) : [];

  const defs: SectionDef[] = [
    { id: "review_requested", label: "On you", title: "Waiting on your review", why: why("review_requested"),
      count: onYou.length, urgent: true, body: () => <PRTable rows={onYou} /> },
    { id: "ready", label: "Ready to merge", title: "Ready to merge",
      why: qaInUse
        ? "Reviewed, approved, green, and QA accepted. Nothing is left to do but press the button."
        : "Approved with nothing actually failing. A red policy check does not keep a pull request out of this list.",
      count: ready.length, body: () => <PRTable rows={ready} /> },
    ...(qaInUse
      ? [{ id: "awaiting_qa", label: "Ready to QA", title: "Waiting on QA",
          why: "Reviewed, approved and green - the QA label is the only thing left. These are the ones to hand over, not to chase the author about.",
          count: awaitingQA.length, body: () => <PRTable rows={awaitingQA} /> }]
      : []),
    { id: "unreviewed", label: "Unclaimed", title: "Nobody has picked these up", why: why("unreviewed"),
      count: unclaimed.length, body: () => <PRTable rows={unclaimed} /> },
    { id: "my_prs", label: "Yours", title: "Your open pull requests", why: why("my_prs"),
      count: mine.length, body: () => <PRTable rows={mine} showAuthor={false} /> },
    { id: "merge_readiness", label: "All open", title: "Open pull requests", why: why("merge_readiness"),
      count: allOpen.length, body: () => <PRTable rows={allOpen} /> },
    { id: "stale", label: "Stale", title: "Abandoned work",
      why: "Pull requests and branches that have stopped moving. Long-lived branches drift from main and get harder to merge the longer they sit - finish them or close them.",
      count: (stale?.rows?.length ?? 0) + branches.length,
      body: () => <StaleView prs={stale?.rows ?? []} botCount={stale?.bot_count ?? 0} branches={branches} /> },
    { id: "security", label: "Security", title: "Security alerts",
      why: "Open Dependabot and code-scanning alerts. Vendored dependencies are counted in the totals but kept out of the per-repository rollup, so a lockfile in node_modules cannot outrank your own code.",
      count: security?.dependabot?.criticals?.length ?? 0, urgent: true,
      body: () => (security ? <Security data={security} /> : <Empty />) },
  ];

  const sections: Section[] = [
    { id: "overview", label: "Overview" },
    ...defs.map((d) => ({ id: d.id, label: d.label, count: d.count, urgent: d.urgent })),
  ];

  // The overview stacks the same sections, ordered by how much each one
  // is the reader's move. Security is excluded: it is a different shape
  // and a different question, and belongs in its own tab.
  const blocks: Block[] = defs
    .filter((d) => !["merge_readiness"].includes(d.id))
    .map((d, i) => ({
      id: d.id,
      title: d.title,
      why: d.why,
      count: d.id === "security" ? critHigh : d.count,
      priority: 100 - i * 10,
      urgent: d.urgent,
      render:
        d.id === "security"
          ? () => (security ? <SecuritySummary data={security} onOpen={() => setActive("security")} /> : <Empty />)
          : d.id === "stale"
          ? (limit: number) => (
              <StaleView
                prs={(stale?.rows ?? []).slice(0, limit)}
                botCount={stale?.bot_count ?? 0}
                branches={branches.slice(0, limit)}
              />
            )
          : preview.prs(
              d.id === "review_requested" ? onYou
              : d.id === "ready" ? ready
              : d.id === "awaiting_qa" ? awaitingQA
              : d.id === "unreviewed" ? unclaimed
              : mine,
              d.id !== "my_prs",
            ),
    }));

  const stats: Stat[] = useMemo(() => [
    { label: "On you", value: onYou.length, hint: "reviews requested from you", tone: "critical", onClick: () => setActive("review_requested") },
    { label: "Ready to merge", value: ready.length, hint: qaInUse ? "approved, green, QA accepted" : "approved and green", tone: "good", onClick: () => setActive("ready") },
    ...(qaInUse ? [{ label: "Ready to QA", value: awaitingQA.length, hint: "only the QA label is missing", tone: "warning" as const, onClick: () => setActive("awaiting_qa") }] : []),
    { label: "Unclaimed", value: unclaimed.length, hint: "no reviewer assigned", tone: "warning", onClick: () => setActive("unreviewed") },
    { label: "Stale", value: stale?.rows?.length ?? 0, hint: `${stale?.bot_count ?? 0} bot PRs not shown`, onClick: () => setActive("stale_prs") },
    { label: "Critical alerts", value: security?.dependabot?.criticals?.length ?? 0,
      hint: "excludes vendored dependencies", tone: "critical", onClick: () => setActive("security") },
    // eslint-disable-next-line react-hooks/exhaustive-deps
  ], [data]);

  const def = defs.find((d) => d.id === active);
  const resultKey = active === "ready" ? "merge_readiness" : active === "stale" ? "stale_prs" : active;
  const result = data?.results[resultKey];

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
            <div className="flex items-center gap-2">
              <span className="mr-0.5 text-xs" style={{ color: "var(--faint)" }}>
                {data?.sweeping ? "sweeping…" : data?.ready ? `as of ${ago(data.age_seconds)}` : "first sweep…"}
              </span>
              <button
                onClick={() => refresh.mutate()}
                disabled={data?.sweeping}
                className="flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-sm font-medium disabled:opacity-50"
                style={{ background: "var(--surface)", border: "1px solid var(--line)", boxShadow: "var(--shadow)" }}
              >
                <Icon.refresh />
                Refresh
              </button>
              <button
                onClick={() => setActive(active === "__rules" ? "overview" : "__rules")}
                aria-pressed={active === "__rules"}
                title="How each check is defined. Read-only - changes are made in your .env"
                className="flex items-center gap-1.5 rounded-lg px-2.5 py-1.5 text-sm font-medium"
                style={{
                  background: active === "__rules" ? "var(--surface-2)" : "var(--surface)",
                  border: "1px solid var(--line)",
                  boxShadow: "var(--shadow)",
                  color: active === "__rules" ? "var(--ink)" : "var(--muted)",
                }}
              >
                <Icon.info />
                Rules
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
              {active === "overview" && (
                <div className="card mb-6 overflow-hidden">
                  <StatTiles stats={stats} />
                </div>
              )}

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
