import { useMemo, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
  saveCapacity, saveSprintReview,
  type SprintPerson, type SprintReport,
} from "../api";
import { Badge } from "./Badge";
import { NumberField, SaveBar, SidePanel, parse, stateOf } from "./Panel";
import { formatDay } from "../dates";

/* This sprint's capacity, and nothing else.
 *
 * This panel used to carry two tables: who was away this sprint, and what
 * each person is expected to deliver in general. They are different kinds
 * of thing - one is entered fresh every fortnight, the other is set once
 * and revisited when somebody joins or goes part-time - and holding them
 * together made the durable one look like a form you had to fill in
 * again. The roster and the baselines have moved behind the gear, beside
 * Refresh; what is left here is the sprint.
 *
 * Editing is explicit: nothing is written as you type, and Save says
 * plainly whether the panel is saved, unsaved or failed. See Panel.tsx,
 * which holds the pieces both panels are built from so the two cannot
 * come to behave differently. */

/** What has been typed but not yet written, kept per sprint so that
 *  changing sprint mid-edit preserves the work rather than binning it. */
type SprintDraft = {
  number: number;
  fields: Record<string, { planned?: string; unplanned?: string }>;
};

function ReviewBadge({ report }: { report: SprintReport }) {
  const r = report.capacity_review;
  if (r.state === "adjusted") {
    return <Badge tone="good" label={`Reviewed · ${r.adjusted} away`} />;
  }
  if (r.state === "no_adjustments") {
    return <Badge tone="good" label="Reviewed · nobody was away" />;
  }
  return <Badge tone="warning" label="Not reviewed" />;
}

export function CapacityPanel({
  report, onClose, onOpenSettings,
}: {
  report: SprintReport;
  onClose: () => void;
  /** Sending somebody to where baselines live, rather than telling them
   *  it is elsewhere and leaving them to find it. */
  onOpenSettings: () => void;
}) {
  const qc = useQueryClient();

  const sprintID = report.sprint.jira_id;

  // Drafts survive a refetch of the report and a change of sprint. They
  // are cleared only by a save that succeeded, or by discarding them.
  const [drafts, setDrafts] = useState<Record<number, SprintDraft>>({});

  const here = drafts[sprintID]?.fields ?? {};

  /** Setting a field back to the stored value drops the draft, so
   *  "unsaved changes" never counts a change that undid itself. */
  const setDay = (p: SprintPerson, which: "planned" | "unplanned", raw: string) => {
    const stored = which === "planned" ? p.planned_days_off : p.unplanned_days_off;
    setDrafts((prev) => {
      const sprint = prev[sprintID] ?? { number: report.sprint.number, fields: {} };
      const row = { ...(sprint.fields[p.account_id] ?? {}), [which]: raw };
      if (parse(raw) === stored) delete row[which];

      const fields = { ...sprint.fields };
      if (Object.keys(row).length === 0) delete fields[p.account_id];
      else fields[p.account_id] = row;

      const next = { ...prev };
      if (Object.keys(fields).length === 0) delete next[sprintID];
      else next[sprintID] = { number: report.sprint.number, fields };
      return next;
    });
  };

  // What Save would write: the draft where there is one, the stored value
  // where there is not. Both days are read from the same place at the
  // same moment, so editing one can no longer write back a stale copy of
  // the other - which is how an edit used to be lost.
  const daysOf = (p: SprintPerson) => ({
    planned: here[p.account_id]?.planned !== undefined
      ? parse(here[p.account_id].planned!) : p.planned_days_off,
    unplanned: here[p.account_id]?.unplanned !== undefined
      ? parse(here[p.account_id].unplanned!) : p.unplanned_days_off,
  });

  /* Who this panel is for.
   *
   * Only somebody on the roster, opted in, and with a baseline. Days off
   * are subtracted from a baseline, so for anybody else there is no
   * arithmetic to do and the row was a name, two greyed-out boxes and a
   * dash - which reads as something you have failed to fill in rather
   * than as something that does not apply.
   *
   * The three kinds left out are all handled elsewhere, and better:
   * somebody never added raises the off-roster flag, somebody opted out
   * has already been answered (offering their day fields would contradict
   * the opt-out), and somebody opted in with no baseline raises its own
   * flag. All three are managed in Settings, which is where they can
   * actually be changed. */
  const measured = useMemo(() => report.people.filter((p) => p.measured), [report.people]);

  // Anybody who delivered here and is not measured. Not a row - a line
  // saying why the table is shorter than the report, and where to go.
  const elsewhereOnThePage = report.people.filter((p) => !p.measured && p.delivered > 0);

  const changedPeople = useMemo(
    () => measured.filter((p) => here[p.account_id] !== undefined),
    [measured, here],
  );
  const capacityInvalid = changedPeople.some((p) => {
    const d = daysOf(p);
    return Number.isNaN(d.planned) || Number.isNaN(d.unplanned);
  });

  const elsewhere = Object.entries(drafts)
    .filter(([id]) => Number(id) !== sprintID)
    .map(([, d]) => d.number);

  const pending = changedPeople.length + elsewhere.length > 0;

  const invalidate = () => {
    // The server folds a saved figure into the report before it answers,
    // so this refetch comes back with the new numbers rather than with
    // the old ones and a promise.
    qc.invalidateQueries({ queryKey: ["sprint-report"] });
    qc.invalidateQueries({ queryKey: ["sprints"] });
  };

  const capacity = useMutation({
    mutationFn: async () => {
      for (const p of changedPeople) {
        const d = daysOf(p);
        await saveCapacity({
          sprint_jira_id: sprintID, account_id: p.account_id,
          planned_days_off: d.planned, unplanned_days_off: d.unplanned, reviewed: true,
        });
      }
      // Pressing Save is the review: somebody has been through this
      // sprint's availability and written down what they found.
      await saveSprintReview(sprintID, true);
    },
    onSuccess: () => {
      setDrafts((prev) => {
        const next = { ...prev };
        delete next[sprintID];
        return next;
      });
      invalidate();
    },
  });

  // Recording "nobody was away" is a statement in its own right, so it is
  // its own button rather than a save of nothing.
  const review = useMutation({
    mutationFn: (reviewed: boolean) => saveSprintReview(sprintID, reviewed),
    onSuccess: invalidate,
  });

  const reviewed = report.capacity_review.state !== "not_reviewed";

  return (
    <SidePanel
      title={`Sprint ${report.sprint.number} capacity`}
      subtitle="Who was away, in working days. This sprint only."
      pending={pending}
      onClose={onClose}
      banner={
        elsewhere.length > 0 ? (
          <p
            className="mx-5 mb-3 rounded-lg px-3 py-2 text-[12.5px]"
            style={{ background: "var(--warn-bg)", color: "var(--warn)" }}
          >
            You have unsaved changes to sprint {elsewhere.join(", ")}. They are kept here until you go
            back to that sprint and save them.
          </p>
        ) : undefined
      }
      footer={
        <SaveBar
          dirty={changedPeople.length}
          invalid={capacityInvalid}
          state={stateOf(capacity)}
          error={capacity.error instanceof Error ? capacity.error.message : undefined}
          onSave={() => capacity.mutate()}
        >
          {/* The answer that had nowhere to go before: everybody was
              here, so there is nothing to type. */}
          {!reviewed && changedPeople.length === 0 && measured.length > 0 && (
            <button
              onClick={() => review.mutate(true)}
              disabled={review.isPending}
              className="rounded-lg px-3 py-1.5 text-sm font-medium disabled:opacity-50"
              style={{ background: "var(--surface-2)", border: "1px solid var(--line)", color: "var(--ink)" }}
            >
              {report.capacity_review.adjusted === 0 ? "Everyone was available" : "Mark reviewed"}
            </button>
          )}
        </SaveBar>
      }
    >
      <p className="mb-3 text-[13px]" style={{ color: "var(--muted)" }}>
        Capacity is the baseline less <em>planned</em> leave — so this is the number delivery is
        actually measured against. Unplanned absence is not taken off it: a sprint commits to what it
        knew, and taking the illness off afterwards would erase the miss it caused. It is reported
        against the shortfall instead.
      </p>

      {measured.length === 0 ? (
        /* Nobody to type a day off for. An empty table with headings
           looks like a list that failed to load; this says what is
           missing and opens the place it is fixed. */
        <div
          className="rounded-lg px-4 py-6 text-center"
          style={{ background: "var(--surface-2)" }}
        >
          <p className="text-[13px] font-medium">Nobody is being measured this sprint.</p>
          <p className="mx-auto mt-1 max-w-sm text-[12.5px]" style={{ color: "var(--muted)" }}>
            Capacity is a baseline less planned leave, so it needs somebody on the roster, opted in,
            with a baseline set. Delivery is still reported for everyone either way.
          </p>
          <button
            onClick={onOpenSettings}
            className="mt-3 rounded-lg px-3 py-1.5 text-[13px] font-medium"
            style={{ background: "var(--accent)", color: "#fff" }}
          >
            Open settings
          </button>
        </div>
      ) : (
        <>
          <div
            className="mb-4 rounded-lg px-3 py-2 text-[12.5px]"
            style={{ background: "var(--surface-2)", color: "var(--muted)" }}
          >
            <strong style={{ color: "var(--ink)" }}>Planned</strong> was known before the sprint began —
            booked leave, a public holiday, agreed part-time days.{" "}
            <strong style={{ color: "var(--ink)" }}>Unplanned</strong> came up during it, and is what
            explains a shortfall nobody could have planned around. The reason is deliberately not recorded.
          </div>

          <div className="mb-4 flex flex-wrap items-center gap-2">
            <ReviewBadge report={report} />
            <span className="flex-1 text-[12px]" style={{ color: "var(--muted)" }}>
              {report.capacity_review.state === "not_reviewed"
                ? "Nobody has confirmed this sprint's availability yet."
                : `Confirmed ${formatDay(report.capacity_review.reviewed_at)}.`}
            </span>
            {reviewed && (
              <button
                onClick={() => review.mutate(false)}
                disabled={review.isPending}
                className="rounded px-2 py-0.5 text-[12px] disabled:opacity-50"
                style={{ color: "var(--muted)", border: "1px solid var(--line)" }}
              >
                Reopen
              </button>
            )}
          </div>

          <table className="w-full border-collapse">
            <thead>
              <tr>
                <th className="pb-2 text-left text-[11px] font-semibold tracking-wide uppercase" style={{ color: "var(--faint)" }}>Person</th>
                {["Planned", "Unplanned", "Capacity"].map((h) => (
                  <th key={h} className="pb-2 text-right text-[11px] font-semibold tracking-wide uppercase" style={{ color: "var(--faint)" }}>{h}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {measured.map((p) => {
                const d = daysOf(p);
                // The capacity this row will have once saved, so the
                // arithmetic on screen matches the fields above it.
                const off = Number.isNaN(d.planned) || Number.isNaN(d.unplanned)
                  ? NaN : d.planned + d.unplanned;
                // One point is one working day, so a day off costs a
                // point. This must match internal/sprint/build.go: if the
                // two drift, the figure changes the moment you save.
                const cap = !Number.isNaN(off)
                  ? Math.max(0, p.baseline - off)
                  : NaN;
                return (
                  <tr key={p.account_id} className="border-t" style={{ borderColor: "var(--line)" }}>
                    <td className="py-2.5 pr-3 text-[13px]">{p.name}</td>
                    <td className="py-2.5 text-right">
                      <NumberField
                        label={`Planned days off for ${p.name}`}
                        value={p.planned_days_off}
                        draft={here[p.account_id]?.planned}
                        onChange={(s) => setDay(p, "planned", s)}
                        width="w-14"
                      />
                      <span className="ml-1 text-[11px]" style={{ color: "var(--faint)" }}>d</span>
                    </td>
                    <td className="py-2.5 text-right">
                      <NumberField
                        label={`Unplanned days off for ${p.name}`}
                        value={p.unplanned_days_off}
                        draft={here[p.account_id]?.unplanned}
                        onChange={(s) => setDay(p, "unplanned", s)}
                        width="w-14"
                      />
                      <span className="ml-1 text-[11px]" style={{ color: "var(--faint)" }}>d</span>
                    </td>
                    <td className="tnum py-2.5 text-right text-[13px] font-semibold">
                      {Number.isNaN(cap) ? "—" : `${cap.toFixed(1)} pts`}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>

          <p className="mt-4 text-[12px]" style={{ color: "var(--faint)" }}>
              One point is one working day, so a day off costs one point: somebody on 6
              points losing 3 days has a capacity of 3.0. Capacity never goes below zero.
          </p>
        </>
      )}

      {/* Why this table is shorter than the report. Said once, with the
          way to change it, rather than as a row per person that cannot be
          edited here. */}
      {elsewhereOnThePage.length > 0 && (
        <div
          className="mt-4 flex flex-wrap items-center gap-2 rounded-lg px-3 py-2 text-[12.5px]"
          style={{ background: "var(--surface-2)", color: "var(--muted)" }}
        >
          <span className="flex-1">
            {elsewhereOnThePage.length} other {elsewhereOnThePage.length === 1 ? "person" : "people"}{" "}
            delivered work this sprint without being measured — not on the roster, opted out, or with no
            baseline. Their points still count towards the sprint; there is simply no capacity to adjust.
          </span>
          <button
            onClick={onOpenSettings}
            className="shrink-0 rounded px-2 py-1 text-[12px] font-medium"
            style={{ border: "1px solid var(--line)", color: "var(--ink)" }}
          >
            Open settings
          </button>
        </div>
      )}
    </SidePanel>
  );
}
