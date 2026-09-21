import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { fetchSprintReport, fetchSprints } from "./api";
import { SprintPicker } from "./components/SprintPicker";
import { SprintReportView } from "./components/SprintReport";
import { Icon } from "./components/Icons";
import { CapacityPanel } from "./components/CapacityPanel";

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
  const [picked, setPicked] = useState<number | null>(null);
  const [editing, setEditing] = useState(false);

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
      setPicked(current.number);
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
              {res.report.sprint.starts?.slice(0, 10)} to {res.report.sprint.ends?.slice(0, 10)}
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
            onClick={() => setEditing(true)}
            disabled={!res}
            className="flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-sm font-medium disabled:opacity-50"
            style={{ background: "var(--surface)", border: "1px solid var(--line)", boxShadow: "var(--shadow)" }}
          >
            <Icon.sliders />
            Baselines
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
        onPick={setPicked}
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

      {res && !report.isLoading && <SprintReportView report={res.report} />}

      {editing && res && <CapacityPanel report={res.report} onClose={() => setEditing(false)} />}
    </div>
  );
}
