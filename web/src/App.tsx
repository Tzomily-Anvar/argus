import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { fetchRules, fetchSnapshot, requestRefresh, type BranchRow, type MergeReadiness, type PRRow, type RuleInfo } from "./api";
import { StatTiles, type Stat } from "./components/StatTiles";
import { BranchTable, Empty, PRTable } from "./components/Tables";
import { Security, type SecurityData } from "./components/Security";
import { RulesPanel } from "./components/RulesPanel";
import { PolicyHintBanner } from "./components/PolicyHint";

function ago(seconds: number): string {
  if (seconds < 60) return "just now";
  const m = Math.floor(seconds / 60);
  if (m < 60) return `${m} min ago`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ago`;
  return `${Math.floor(h / 24)}d ago`;
}

function useTheme() {
  const [theme, setTheme] = useState<"system" | "light" | "dark">(() => {
    try {
      return (localStorage.getItem("argus-theme") as "system" | "light" | "dark") ?? "system";
    } catch {
      return "system";
    }
  });
  useEffect(() => {
    const root = document.documentElement;
    if (theme === "system") root.removeAttribute("data-theme");
    else root.setAttribute("data-theme", theme);
    try {
      localStorage.setItem("argus-theme", theme);
    } catch {
      /* private browsing: the page still renders correctly */
    }
  }, [theme]);
  return { theme, setTheme };
}

type Tab = { id: string; label: string; count: number };

export default function App() {
  const qc = useQueryClient();
  const { theme, setTheme } = useTheme();
  const [active, setActive] = useState<string>("review_requested");

  const snapshot = useQuery({
    queryKey: ["snapshot"],
    queryFn: fetchSnapshot,
    // While a sweep is running, poll briskly so the page settles on its
    // own; otherwise idle back so a tab left open all day costs nothing.
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
    if (!r || r.error) return [];
    return (Array.isArray(r.data) ? r.data : []) as T[];
  };

  const stale = data?.results["stale_prs"]?.data as { rows?: PRRow[]; bot_count?: number } | undefined;
  const merge = data?.results["merge_readiness"]?.data as MergeReadiness | undefined;
  const mergeRows = merge?.rows ?? [];
  const security = data?.results["security"]?.data as SecurityData | undefined;
  const mergeable = mergeRows.filter((p) => p.review_decision === "APPROVED" && p.checks_green);

  const stats: Stat[] = useMemo(
    () => [
      {
        label: "On you",
        value: rows<PRRow>("review_requested").length,
        hint: "reviews requested from you",
        tone: "critical",
        onClick: () => setActive("review_requested"),
      },
      {
        label: "Ready to merge",
        value: mergeable.length,
        hint: "approved and green",
        onClick: () => setActive("merge_readiness"),
      },
      {
        label: "Unclaimed",
        value: rows<PRRow>("unreviewed").length,
        hint: "no reviewer assigned",
        tone: "warning",
        onClick: () => setActive("unreviewed"),
      },
      {
        label: "Stale",
        value: stale?.rows?.length ?? 0,
        hint: `${stale?.bot_count ?? 0} bot PRs not shown`,
        onClick: () => setActive("stale_prs"),
      },
      {
        label: "Critical alerts",
        value: security?.dependabot?.criticals?.length ?? 0,
        hint: "first-party code only",
        tone: "critical",
        onClick: () => setActive("security"),
      },
    ],
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [data],
  );

  const tabs: Tab[] = [
    { id: "review_requested", label: "On you", count: rows<PRRow>("review_requested").length },
    { id: "my_prs", label: "Yours", count: rows<PRRow>("my_prs").length },
    { id: "unreviewed", label: "Unclaimed", count: rows<PRRow>("unreviewed").length },
    { id: "merge_readiness", label: "All open", count: mergeRows.length },
    { id: "stale_prs", label: "Stale", count: stale?.rows?.length ?? 0 },
    { id: "stale_branches", label: "Branches", count: rows<BranchRow>("stale_branches").length },
    { id: "security", label: "Security", count: security?.dependabot?.criticals?.length ?? 0 },
    { id: "__rules", label: "Rules", count: ruleList.length },
  ];

  const activeRule = ruleList.find((r) => r.id === active);
  const activeResult = data?.results[active];

  return (
    <div className="mx-auto max-w-6xl px-4 pb-24">
      <header
        className="flex flex-wrap items-center justify-between gap-3 border-b py-4"
        style={{ borderColor: "var(--color-line)" }}
      >
        <div className="flex items-baseline gap-3">
          <h1 className="text-xl font-semibold tracking-tight">Argus</h1>
          {data?.org && (
            <span className="font-mono text-xs" style={{ color: "var(--color-ink-muted)" }}>
              {data.org} · {data.user}
            </span>
          )}
        </div>

        <div className="flex items-center gap-3">
          <span className="text-xs" style={{ color: "var(--color-ink-faint)" }}>
            {data?.sweeping ? "sweeping…" : data?.ready ? `as of ${ago(data.age_seconds)}` : "first sweep…"}
          </span>
          <button
            onClick={() => refresh.mutate()}
            disabled={data?.sweeping}
            className="rounded px-2.5 py-1 text-sm disabled:opacity-50"
            style={{ background: "var(--color-surface-sunken)", boxShadow: "inset 0 0 0 1px var(--color-line)" }}
          >
            Refresh
          </button>
          <select
            value={theme}
            onChange={(e) => setTheme(e.target.value as "system" | "light" | "dark")}
            aria-label="Colour theme"
            className="rounded px-1.5 py-1 text-xs"
            style={{ background: "var(--color-surface-sunken)", boxShadow: "inset 0 0 0 1px var(--color-line)", color: "var(--color-ink-muted)" }}
          >
            <option value="system">Auto</option>
            <option value="light">Light</option>
            <option value="dark">Dark</option>
          </select>
        </div>
      </header>

      {data?.error && (
        <p className="mt-4 rounded px-3 py-2 text-sm" style={{ background: "#fae7e7", color: "#7a1f1f" }}>
          {data.error}
        </p>
      )}

      {!data?.ready && !data?.error && (
        <p className="mt-6 text-sm" style={{ color: "var(--color-ink-muted)" }}>
          Running the first sweep. This takes a few seconds; afterwards the dashboard is served from
          memory and loads instantly.
        </p>
      )}

      {data?.ready && (
        <>
          <div className="mt-5 overflow-hidden rounded" style={{ boxShadow: "0 0 0 1px var(--color-line)" }}>
            <StatTiles stats={stats} />
          </div>

          <nav className="mt-6 flex flex-wrap gap-1 border-b pb-px" style={{ borderColor: "var(--color-line)" }}>
            {tabs.map((t) => {
              const on = t.id === active;
              return (
                <button
                  key={t.id}
                  onClick={() => setActive(t.id)}
                  className="rounded-t px-3 py-1.5 text-sm"
                  style={{
                    color: on ? "var(--color-ink)" : "var(--color-ink-muted)",
                    background: on ? "var(--color-surface-raised)" : "transparent",
                    boxShadow: on ? "inset 0 0 0 1px var(--color-line)" : "none",
                    fontWeight: on ? 600 : 400,
                  }}
                >
                  {t.label}
                  <span className="tnum ml-1.5 text-xs" style={{ color: "var(--color-ink-faint)" }}>
                    {t.count}
                  </span>
                </button>
              );
            })}
          </nav>

          <section className="mt-5">
            {active === "__rules" ? (
              <RulesPanel rules={ruleList} />
            ) : (
              <>
                {activeRule && (
                  <div className="mb-3">
                    <h2 className="font-semibold">{activeRule.title}</h2>
                    <p className="text-sm" style={{ color: "var(--color-ink-muted)" }}>
                      {activeRule.why}
                    </p>
                  </div>
                )}

                {active === "merge_readiness" && merge?.policy_hints && (
                  <PolicyHintBanner hints={merge.policy_hints} />
                )}

                {activeResult?.error ? (
                  <p className="rounded px-3 py-2 text-sm" style={{ background: "#fdeae1", color: "#7d3517" }}>
                    {activeResult.error}
                  </p>
                ) : (
                  <div className="rounded" style={{ background: "var(--color-surface-raised)", boxShadow: "0 0 0 1px var(--color-line)" }}>
                    {active === "security" && security ? (
                      <Security data={security} />
                    ) : active === "stale_branches" ? (
                      <BranchTable rows={rows<BranchRow>("stale_branches")} />
                    ) : active === "stale_prs" ? (
                      <PRTable rows={stale?.rows ?? []} />
                    ) : active === "my_prs" ? (
                      <PRTable rows={rows<PRRow>("my_prs")} showAuthor={false} />
                    ) : active === "merge_readiness" ? (
                      <PRTable rows={mergeRows} />
                    ) : ["review_requested", "unreviewed"].includes(active) ? (
                      <PRTable rows={rows<PRRow>(active)} />
                    ) : (
                      <Empty />
                    )}
                  </div>
                )}

                {activeResult && (
                  <p className="mt-2 text-xs" style={{ color: "var(--color-ink-faint)" }}>
                    swept in {activeResult.duration_ms} ms
                  </p>
                )}
              </>
            )}
          </section>
        </>
      )}
    </div>
  );
}
