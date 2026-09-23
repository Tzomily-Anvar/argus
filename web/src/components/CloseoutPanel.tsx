import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { fetchCloseout, fetchConventions, fetchPeople, type SprintReport } from "../api";
import { useChangeDraft } from "../changes";
import { Badge } from "./Badge";
import { SizeStep, sizeSkipID } from "./closeout/SizeStep";
import { AssignStep, assignSkipID } from "./closeout/AssignStep";
import { EffortStep, effortSkipID } from "./closeout/EffortStep";
import { StoriesStep, storySkipID } from "./closeout/StoriesStep";
import { ReviewStep } from "./closeout/ReviewStep";
import { ReasonsStep } from "./closeout/ReasonsStep";
import { StepIntro, type Skips } from "./closeout/Table";
import { formatDay } from "../dates";

/* Closing out a sprint, as one sitting.
 *
 * A sprint close on this team is six jobs done together, and the first
 * cut of the write-back scattered them where the flags happened to be.
 * This panel walks them: a rail of seven steps, each a table of rows
 * pre-filled with Argus's best suggestion to accept, change or skip, all
 * feeding one draft, one preview, one apply. The draft lives in the
 * store, so the panel can be closed at step three and the close resumed
 * after a reload.
 *
 * It is wider than the other two panels because its steps are tables
 * with an input column, and it has its own shell rather than SidePanel's
 * because the draft persists as it is typed: there is no unsaved work to
 * confirm the loss of, so Done simply closes. */

type StepID = "availability" | "size" | "assign" | "effort" | "stories" | "review" | "reasons";

const STEPS: { id: StepID; title: string }[] = [
  { id: "availability", title: "Availability" },
  { id: "size", title: "Size what finished" },
  { id: "assign", title: "Assign what finished" },
  { id: "effort", title: "Effort on carryover" },
  { id: "stories", title: "Stories wrapping up" },
  { id: "review", title: "Review and apply" },
  { id: "reasons", title: "Reasons and publish" },
];

/** The glyph on the header button: a list with its boxes ticked. */
export const CloseoutGlyph = () => (
  <svg width={15} height={15} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.8} strokeLinecap="round" strokeLinejoin="round">
    <path d="M3.5 6l1.5 1.5L7.5 5M10 6h10M3.5 12l1.5 1.5L7.5 11M10 12h10M3.5 18l1.5 1.5L7.5 17M10 18h10" />
  </svg>
);

/** What each step still has to say for itself: how many rows nobody has
 *  decided on, and how many are queued for the preview. Two badges, not
 *  one, because a step with three rows queued and none outstanding is
 *  finished, and a step with nothing at all was never needed. */
type Counts = { outstanding: number; queued: number };

function StepBadges({ c }: { c: Counts }) {
  return (
    <span className="flex shrink-0 items-center gap-1">
      {c.outstanding > 0 && <Badge tone="warning" label={String(c.outstanding)} title={`${c.outstanding} to decide`} />}
      {c.queued > 0 && <Badge tone="info" label={`${c.queued} queued`} />}
      {c.outstanding === 0 && c.queued === 0 && <Badge tone="neutral" label="—" title="Nothing to do here" />}
    </span>
  );
}

function AvailabilityStep({ report, onOpenCapacity }: { report: SprintReport; onOpenCapacity: () => void }) {
  const r = report.capacity_review;
  const reviewed = r.state !== "not_reviewed";
  return (
    <>
      <StepIntro title="Availability">
        Who was away, in working days, so delivery is measured against the capacity people
        actually had. It is entered in the Capacity panel; opening it closes this one, and the
        draft is kept.
      </StepIntro>
      <div className="flex flex-wrap items-center gap-3 rounded-lg px-4 py-3" style={{ background: "var(--surface-2)" }}>
        {r.state === "adjusted" && <Badge tone="good" label={`Reviewed · ${r.adjusted} away`} />}
        {r.state === "no_adjustments" && <Badge tone="good" label="Reviewed · nobody was away" />}
        {!reviewed && <Badge tone="warning" label="Not reviewed" />}
        <span className="flex-1 text-[12.5px]" style={{ color: "var(--muted)" }}>
          {reviewed ? `Confirmed ${formatDay(r.reviewed_at)}.` : "Nobody has confirmed this sprint's availability yet."}
        </span>
        <button
          onClick={onOpenCapacity}
          className="rounded-lg px-3 py-1.5 text-[13px] font-medium"
          style={{ background: "var(--accent)", color: "#fff" }}
        >
          Open Capacity
        </button>
      </div>
    </>
  );
}

export function CloseoutPanel({
  report, onClose, onOpenCapacity,
}: {
  report: SprintReport;
  onClose: () => void;
  onOpenCapacity: () => void;
}) {
  const qc = useQueryClient();
  const sprintNumber = report.sprint.number;
  const sprintID = report.sprint.jira_id;

  const model = useQuery({ queryKey: ["closeout", sprintNumber], queryFn: () => fetchCloseout(sprintNumber) });
  const roster = useQuery({ queryKey: ["people"], queryFn: fetchPeople });
  const conv = useQuery({ queryKey: ["conventions"], queryFn: fetchConventions, staleTime: Infinity });
  const draft = useChangeDraft(sprintNumber, sprintID);

  // Start where the work is: availability if nobody has confirmed it,
  // otherwise the first table.
  const [step, setStep] = useState<StepID>(report.capacity_review.state === "not_reviewed" ? "availability" : "size");

  // Rows left alone this sitting. Held here rather than in each step so
  // a skip survives moving between steps; see Table.tsx for why it is
  // not written to the store.
  const [skipped, setSkipped] = useState<Record<string, true>>({});
  const skips: Skips = {
    has: (id) => skipped[id] === true,
    set: (id, on) =>
      setSkipped((prev) => {
        const next = { ...prev };
        if (on) next[id] = true;
        else delete next[id];
        return next;
      }),
  };

  const m = model.data;
  const people = roster.data?.people ?? [];
  const decided = (id: string, isQueued: boolean) => isQueued || skips.has(id);
  const tally = <T,>(rows: T[], idOf: (r: T) => string, isQueued: (r: T) => boolean): Counts => {
    const queued = rows.filter(isQueued).length;
    const outstanding = rows.filter((r) => !decided(idOf(r), isQueued(r))).length;
    return { outstanding, queued };
  };
  const hasWorklog = (key: string) => draft.forKey(key).some((q) => q.request.op.startsWith("worklog."));
  const measured = report.people.filter((p) => p.measured);
  const counts: Record<StepID, Counts> = {
    availability: { outstanding: report.capacity_review.state === "not_reviewed" ? 1 : 0, queued: 0 },
    size: tally(m?.size ?? [], (r) => sizeSkipID(r.key), (r) => !!draft.find(r.key, "points.set")),
    assign: tally(m?.assign ?? [], (r) => assignSkipID(r.key), (r) => !!draft.find(r.key, "assignee.set")),
    effort: tally(m?.effort ?? [], (r) => effortSkipID(r.key), (r) => hasWorklog(r.key)),
    stories: tally(
      (m?.stories ?? []).filter((r) => r.sum_known && !(r.own_points != null && r.own_points === r.linked_points)),
      (r) => storySkipID(r.key),
      (r) => !!draft.find(r.key, "points.set"),
    ),
    review: { outstanding: 0, queued: draft.list.length },
    reasons: {
      outstanding: measured.filter((p) => !(m?.people.find((c) => c.account_id === p.account_id)?.note ?? p.note ?? "").trim()).length,
      queued: 0,
    },
  };

  // After a batch lands the server has rebuilt the report and cleared
  // the applied rows from the draft; everything derived from either is
  // read again so the counts on the rail move.
  const refresh = () => {
    qc.invalidateQueries({ queryKey: ["sprint-report"] });
    qc.invalidateQueries({ queryKey: ["closeout"] });
    qc.invalidateQueries({ queryKey: ["draft"] });
    qc.invalidateQueries({ queryKey: ["writes"] });
    qc.invalidateQueries({ queryKey: ["sprints"] });
  };

  const queued = draft.list.length;
  const draftNote =
    draft.loading ? ["Reading the draft…", "var(--faint)"]
      : draft.saving === "pending" ? ["Saving the draft…", "var(--warn)"]
        : draft.saving === "error" ? ["Could not save the draft. It is still here; try again by editing a row.", "var(--crit)"]
          : queued > 0 ? [`${queued} ${queued === 1 ? "change" : "changes"} queued · draft saved`, "var(--good)"]
            : ["Nothing queued", "var(--faint)"];

  const content = () => {
    if (step === "availability") return <AvailabilityStep report={report} onOpenCapacity={onOpenCapacity} />;
    if (step === "review") return <ReviewStep sprintNumber={sprintNumber} draft={draft} onApplied={refresh} />;
    if (step === "reasons") return <ReasonsStep report={report} people={m?.people ?? []} onSaved={refresh} />;
    if (model.isLoading) return <p className="text-[13px]" style={{ color: "var(--muted)" }}>Working out what needs closing…</p>;
    if (model.error || !m) {
      return (
        <p className="rounded-lg px-3 py-2 text-[13px]" style={{ background: "var(--crit-bg)", color: "var(--crit)" }}>
          {model.error ? String(model.error) : "The closeout could not be built."}
        </p>
      );
    }
    switch (step) {
      case "size": return <SizeStep rows={m.size} draft={draft} skips={skips} />;
      case "assign": return <AssignStep rows={m.assign} draft={draft} skips={skips} />;
      case "effort": return <EffortStep rows={m.effort} roster={people} hoursPerPoint={conv.data?.hours_per_point} draft={draft} skips={skips} />;
      case "stories": return <StoriesStep rows={m.stories} draft={draft} skips={skips} />;
    }
  };

  return (
    <>
      <div onClick={onClose} className="fixed inset-0 z-40" style={{ background: "rgb(0 0 0 / 0.18)" }} />
      <aside
        className="fixed top-0 right-0 z-50 flex h-full w-[1000px] max-w-[96vw] flex-col"
        style={{ background: "var(--surface)", borderLeft: "1px solid var(--line)", boxShadow: "var(--shadow)" }}
      >
        <header className="flex items-start justify-between gap-3 px-5 pt-5 pb-3">
          <div>
            <h2 className="text-[15px] font-semibold">Close out sprint {sprintNumber}</h2>
            <p className="mt-0.5 text-[12.5px]" style={{ color: "var(--muted)" }}>
              Seven steps, one draft. Accept, change or skip each row; nothing reaches Jira until step 6.
            </p>
          </div>
          <button onClick={onClose} className="shrink-0 rounded-lg px-2.5 py-1 text-sm" style={{ color: "var(--muted)", border: "1px solid var(--line)" }}>
            Done
          </button>
        </header>

        <div className="flex min-h-0 flex-1" style={{ borderTop: "1px solid var(--line)" }}>
          <nav className="w-56 shrink-0 overflow-y-auto py-3" style={{ borderRight: "1px solid var(--line)" }} aria-label="Close-out steps">
            {STEPS.map((s, i) => {
              const on = s.id === step;
              return (
                <button
                  key={s.id}
                  onClick={() => setStep(s.id)}
                  aria-current={on ? "step" : undefined}
                  className="flex w-full items-center gap-2 px-4 py-2 text-left text-[13px]"
                  style={{
                    background: on ? "var(--surface-2)" : "transparent",
                    color: on ? "var(--ink)" : "var(--muted)",
                    fontWeight: on ? 600 : 400,
                    boxShadow: on ? "inset 2px 0 0 var(--accent)" : "none",
                  }}
                >
                  <span className="tnum w-4 shrink-0 text-[11.5px]" style={{ color: "var(--faint)" }}>{i + 1}</span>
                  <span className="flex-1 truncate">{s.title}</span>
                  <StepBadges c={counts[s.id]} />
                </button>
              );
            })}
          </nav>
          <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">{content()}</div>
        </div>

        <div
          className="flex flex-wrap items-center justify-between gap-2 px-5 py-3"
          style={{ borderTop: "1px solid var(--line)", background: "var(--surface)" }}
        >
          <span className="text-[12.5px]" style={{ color: draftNote[1] }}>{draftNote[0]}</span>
          <div className="flex items-center gap-2">
            <button
              onClick={() => draft.clear()}
              disabled={queued === 0}
              className="rounded-lg px-3 py-1.5 text-sm font-medium disabled:opacity-40"
              style={{ background: "var(--surface)", border: "1px solid var(--line)", boxShadow: "var(--shadow)" }}
            >
              Discard draft
            </button>
            {step !== "reasons" && (
              <button
                onClick={() => setStep(STEPS[STEPS.findIndex((s) => s.id === step) + 1].id)}
                className="rounded-lg px-3 py-1.5 text-sm font-medium"
                style={{ background: "var(--accent)", color: "#fff" }}
              >
                Next step
              </button>
            )}
          </div>
        </div>
      </aside>
    </>
  );
}
