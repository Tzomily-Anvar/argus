import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { fetchCalibrationTrend, fetchSprintReport, fetchSprints } from "./api";
import { navigate, useRoute } from "./route";
import { SprintPicker } from "./components/SprintPicker";
import { SprintReportView } from "./components/SprintReport";
import { Icon } from "./components/Icons";
import { CapacityPanel } from "./components/CapacityPanel";
import { SettingsPanel } from "./components/SettingsPanel";
import { formatDateRange } from "./dates";

function ago(iso: string): string {
  const secs = (Date.now() - new Date(iso).getTime()) / 1000;
  if (secs < 60) return "just now";
  const m = Math.floor(secs / 60);
  if (m < 60) return `${m} min ago`;
  const h = Math.floor(m / 60);
  return h < 24 ? `${h}h ago` : `${Math.floor(h / 24)}d ago`;
}

export function SprintTool() {
  const qc = useQueryClient();

  // Two panels, and the split between them is the point. Capacity is
  // about this sprint and is entered every fortnight; settings are about
  // the team and are revisited a few times a year. Only one is ever open,
  // because they are both sheets over the same page.
  const [panel, setPanel] = useState<"none" | "capacity" | "settings">("none");

  // The sprint number is part of the address, so a report can be linked
  // to and a reload comes back to the sprint you were reading. Anything
  // that is not a sprint number reads as "none picked", and the current
  // sprint fills in below.
  const route = useRoute();
  const picked = /^\d+$/.test(route.view) ? Number(route.view) : null;
  const pick = (n: number) => navigate({ tool: "sprint", view: String(n) });

  // The list is fetched live rather than cached: it costs about half a
  // second, and storing it would buy a staleness bug instead.
  const sprints = useQuery({ queryKey: ["sprints"], queryFn: () => fetchSprints(false), staleTime: 60_000 });

  // The whole board, fetched only when someone opens the full list. Most
  // visits never need it, and it is the same half-second either way.
  const [wantAll, setWantAll] = useState(false);
  const allSprints = useQuery({
    queryKey: ["sprints", "all"],
    queryFn: () => fetchSprints(true),
    enabled: wantAll,
    staleTime: 60_000,
  });

  // Default to the current sprint once the list arrives, so the page is
  // never an empty prompt.
  useEffect(() => {
    if (picked === null && sprints.data?.sprints.length) {
      const current = sprints.data.sprints.find((s) => s.current) ?? sprints.data.sprints[0];
      // Replaced, not pushed: nobody chose this, so Back should leave the
      // tool rather than bounce off the sprint it filled in.
      navigate({ tool: "sprint", view: String(current.number) }, true);
    }
  }, [sprints.data, picked]);

  const report = useQuery({
    queryKey: ["sprint-report", picked],
    queryFn: () => fetchSprintReport(picked!),
    enabled: picked !== null,
    // A report already built comes back in milliseconds; one being rebuilt
    // in the background is polled until it settles.
    refetchInterval: (q) => (q.state.data?.building ? 3_000 : false),
  });

  // The estimate-against-actual run, fetched apart from the report.
  //
  // It costs a Jira sweep for every sprint not already held, so asking
  // for it inside the report would make opening a sprint slower for a
  // section further down the page. It comes back with whatever has been
  // swept and says when more is coming, and is polled until it settles -
  // the same contract the report itself has.
  const trend = useQuery({
    queryKey: ["sprint-calibration", picked],
    queryFn: () => fetchCalibrationTrend(picked!),
    enabled: picked !== null,
    refetchInterval: (q) => (q.state.data?.building ? 5_000 : false),
  });

  const refresh = useMutation({
    mutationFn: () => fetchSprintReport(picked!, true),
    onSuccess: (data) => qc.setQueryData(["sprint-report", picked], data),
  });

  const res = report.data;

  return (
    <div className="mx-auto max-w-5xl px-6 pt-6 pb-20">
      <header className="mb-5 flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-lg font-semibold tracking-tight">
            {res ? `Sprint ${res.report.sprint.number} report` : "Sprint reports"}
          </h1>
          {res && (
            <p className="mt-0.5 text-[13px]" style={{ color: "var(--muted)" }}>
              {formatDateRange(res.report.sprint.starts, res.report.sprint.ends, true)}
              {res.report.sprint.provisional && " · still open, so delivery is measured against today"}
            </p>
          )}
        </div>
        <div className="flex items-center gap-2">
          <span className="text-xs" style={{ color: "var(--faint)" }}>
            {refresh.isPending || res?.building
              ? "rebuilding…"
              : res
                ? `as of ${ago(res.built_at)}`
                : ""}
          </span>
          <button
            onClick={() => setPanel("capacity")}
            disabled={!res}
            className="flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-sm font-medium disabled:opacity-50"
            style={{ background: "var(--surface)", border: "1px solid var(--line)", boxShadow: "var(--shadow)" }}
          >
            <Icon.sliders />
            Capacity
          </button>
          {/* The roster and the baselines. Not disabled with no report:
              they are about the team rather than about a sprint, so there
              is nothing to wait for. */}
          <button
            onClick={() => setPanel("settings")}
            aria-label="Settings"
            title="The team, their baselines, and importing the roster"
            className="flex items-center rounded-lg px-2.5 py-2 text-sm font-medium"
            style={{ background: "var(--surface)", border: "1px solid var(--line)", boxShadow: "var(--shadow)" }}
          >
            <Icon.gear />
          </button>
          <button
            onClick={() => refresh.mutate()}
            disabled={picked === null || refresh.isPending}
            className="flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-sm font-medium disabled:opacity-50"
            style={{ background: "var(--surface)", border: "1px solid var(--line)", boxShadow: "var(--shadow)" }}
          >
            <Icon.refresh />
            Refresh
          </button>
        </div>
      </header>

      <SprintPicker
        sprints={sprints.data?.sprints ?? []}
        all={allSprints.data?.sprints ?? null}
        active={picked}
        onPick={pick}
        onNeedAll={() => setWantAll(true)}
        loading={sprints.isLoading}
        loadingAll={allSprints.isLoading}
      />

      {sprints.error && (
        <p className="rounded-xl px-3.5 py-2.5 text-sm" style={{ background: "var(--crit-bg)", color: "var(--crit)" }}>
          Could not reach Jira: {String(sprints.error)}
        </p>
      )}

      {report.error && (
        <p className="rounded-xl px-3.5 py-2.5 text-sm" style={{ background: "var(--crit-bg)", color: "var(--crit)" }}>
          {String(report.error)}
        </p>
      )}

      {report.isLoading && picked !== null && (
        <div className="card p-8 text-center">
          <p className="text-sm font-medium">Building sprint {picked}…</p>
          <p className="mt-1 text-sm" style={{ color: "var(--muted)" }}>
            This sprint has not been swept before. It takes a few seconds; afterwards it opens
            instantly.
          </p>
        </div>
      )}

      {res && !report.isLoading && <SprintReportView report={res.report} trend={trend.data} />}

      {panel === "capacity" && res && (
        <CapacityPanel
          report={res.report}
          onClose={() => setPanel("none")}
          onOpenSettings={() => setPanel("settings")}
        />
      )}
      {panel === "settings" && (
        <SettingsPanel report={res?.report} onClose={() => setPanel("none")} />
      )}
    </div>
  );
}
