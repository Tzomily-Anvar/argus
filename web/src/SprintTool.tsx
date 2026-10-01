import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { fetchCalibrationTrend, fetchSprintReport, fetchSprints } from "./api";
import { navigate, useRoute } from "./route";
import { SprintPicker } from "./components/SprintPicker";
import { SprintReportView } from "./components/SprintReport";
import { Icon } from "./components/Icons";
import { CapacityPanel } from "./components/CapacityPanel";
import { SettingsPanel } from "./components/SettingsPanel";
import { CloseoutGlyph, CloseoutPanel } from "./components/CloseoutPanel";
import { ago, formatDateRange } from "./dates";
import { HeaderButton, ToolHeader } from "./components/ToolHeader";
import { Notice } from "./components/Notice";

export function SprintTool() {
  const qc = useQueryClient();

  // Three panels, and the split between them is the point. Capacity is
  // about this sprint and is entered every fortnight; settings are about
  // the team and are revisited a few times a year; the close-out is the
  // sitting at the end of a sprint that walks the corrections and writes
  // them back. Only one is ever open, because they are all sheets over
  // the same page.
  const [panel, setPanel] = useState<"none" | "capacity" | "settings" | "closeout">("none");

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
      <ToolHeader
        title={res ? `Sprint ${res.report.sprint.number} report` : "Sprint reports"}
        subtitle={res && (
          <>
            {formatDateRange(res.report.sprint.starts, res.report.sprint.ends, true)}
            {res.report.sprint.provisional && " · still open, so delivery is measured against today"}
          </>
        )}
        note={refresh.isPending || res?.building ? "rebuilding…" : res ? `as of ${ago(res.built_at)}` : ""}
        actions={
          <>
            <HeaderButton onClick={() => setPanel("capacity")} disabled={!res}>
              <Icon.sliders />
              Capacity
            </HeaderButton>
            {/* The close-out is built from the report, so it waits for one
                the same way Capacity does. */}
            <HeaderButton
              onClick={() => setPanel("closeout")}
              disabled={!res}
              title="Walk the sprint's close: size, assign, log effort, roll up stories, then write it back"
            >
              <CloseoutGlyph />
              Close out
            </HeaderButton>
            {/* The roster and the baselines. Not disabled with no report:
                they are about the team rather than about a sprint, so there
                is nothing to wait for. */}
            <HeaderButton
              iconOnly
              onClick={() => setPanel("settings")}
              aria-label="Settings"
              title="The team, their baselines, and importing the roster"
            >
              <Icon.gear />
            </HeaderButton>
            <HeaderButton onClick={() => refresh.mutate()} disabled={picked === null || refresh.isPending}>
              <Icon.refresh />
              Refresh
            </HeaderButton>
          </>
        }
      />

      <SprintPicker
        sprints={sprints.data?.sprints ?? []}
        all={allSprints.data?.sprints ?? null}
        active={picked}
        onPick={pick}
        onNeedAll={() => setWantAll(true)}
        loading={sprints.isLoading}
        loadingAll={allSprints.isLoading}
      />

      {sprints.error && <Notice>Could not reach Jira: {String(sprints.error)}</Notice>}

      {report.error && <Notice>{String(report.error)}</Notice>}

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
      {panel === "closeout" && res && (
        <CloseoutPanel
          report={res.report}
          onClose={() => setPanel("none")}
          onOpenCapacity={() => setPanel("capacity")}
        />
      )}
    </div>
  );
}
