import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  fetchConventions, savePerson, saveCapacity,
  type SprintPerson, type SprintReport,
} from "../api";

/* Baselines and capacity, as a panel over the report.
 *
 * These are two different things and were previously one table, which is
 * why nobody could tell them apart:
 *
 *   A baseline is a property of a person. It applies to every sprint and
 *   changes rarely - when someone goes part-time, or joins.
 *
 *   Capacity is a property of a person in ONE sprint. It is the baseline
 *   less whatever time they were away, and it is entered fresh each time.
 *
 * They are now separate sections, and the arithmetic between them is
 * shown rather than implied. */

function NumberField({
  value, onCommit, width = "w-16", step = "0.5",
}: {
  value: number;
  onCommit: (n: number) => void;
  width?: string;
  step?: string;
}) {
  const [draft, setDraft] = useState(String(value));
  const [dirty, setDirty] = useState(false);
  return (
    <input
      type="number"
      step={step}
      min="0"
      value={dirty ? draft : String(value)}
      onChange={(e) => { setDraft(e.target.value); setDirty(true); }}
      onBlur={() => {
        if (!dirty) return;
        const n = parseFloat(draft);
        setDirty(false);
        if (!Number.isNaN(n) && n !== value) onCommit(n);
      }}
      onKeyDown={(e) => e.key === "Enter" && (e.target as HTMLInputElement).blur()}
      className={`tnum ${width} rounded px-2 py-1 text-right text-[13px] outline-none`}
      style={{ background: "var(--surface-2)", border: "1px solid var(--line)", color: "var(--ink)" }}
    />
  );
}

export function CapacityPanel({
  report, onClose,
}: {
  report: SprintReport;
  onClose: () => void;
}) {
  const qc = useQueryClient();
  // Sprint capacity first: it is what changes every sprint. A baseline is
  // set once and revisited rarely, so it belongs behind the thing you came
  // here to do.
  const [tab, setTab] = useState<"baselines" | "sprint">("sprint");
  const conv = useQuery({ queryKey: ["conventions"], queryFn: fetchConventions, staleTime: Infinity });

  const hoursPerPoint = conv.data?.hours_per_point ?? 6;
  const hoursPerDay = conv.data?.hours_per_day ?? 6;
  const sprintDays = conv.data?.sprint_length_days ?? 10;
  const daysPerPoint = hoursPerPoint / hoursPerDay;

  const invalidate = () => {
    qc.invalidateQueries({ queryKey: ["sprint-report"] });
    qc.invalidateQueries({ queryKey: ["sprints"] });
  };
  const person = useMutation({ mutationFn: savePerson, onSuccess: invalidate });
  const capacity = useMutation({ mutationFn: saveCapacity, onSuccess: invalidate });

  const setBaseline = (p: SprintPerson, baseline: number) =>
    person.mutate({ account_id: p.account_id, name: p.name, baseline, active: true });

  const setDays = (p: SprintPerson, planned: number, unplanned: number) =>
    capacity.mutate({
      sprint_jira_id: report.sprint.jira_id, account_id: p.account_id,
      planned_days_off: planned, unplanned_days_off: unplanned, reviewed: true,
    });

  const unregistered = report.people.filter((p) => !p.registered).length;

  return (
    <>
      <div onClick={onClose} className="fixed inset-0 z-40" style={{ background: "rgb(0 0 0 / 0.18)" }} />

      <aside
        className="fixed top-0 right-0 z-50 flex h-full w-[560px] max-w-[94vw] flex-col"
        style={{ background: "var(--surface)", borderLeft: "1px solid var(--line)", boxShadow: "var(--shadow)" }}
      >
        <header className="flex items-start justify-between gap-3 px-5 pt-5 pb-3">
          <h2 className="text-[15px] font-semibold">Capacity</h2>
          <button
            onClick={onClose}
            className="shrink-0 rounded-lg px-2.5 py-1 text-sm"
            style={{ color: "var(--muted)", border: "1px solid var(--line)" }}
          >
            Done
          </button>
        </header>

        <div className="flex gap-1 px-5" style={{ borderBottom: "1px solid var(--line)" }}>
          {([
            ["sprint", `Sprint ${report.sprint.number}`, "this sprint only"],
            ["baselines", "Team baselines", "every sprint"],
          ] as const).map(([id, label, hint]) => (
            <button
              key={id}
              onClick={() => setTab(id)}
              className="relative px-3 py-2 text-left text-[13px]"
              style={{ color: tab === id ? "var(--ink)" : "var(--muted)", fontWeight: tab === id ? 600 : 500 }}
            >
              {label}
              <span className="ml-1.5 text-[11px]" style={{ color: "var(--faint)" }}>{hint}</span>
              {tab === id && (
                <span className="absolute right-0 -bottom-px left-0 h-0.5 rounded-t" style={{ background: "var(--accent)" }} />
              )}
            </button>
          ))}
        </div>

        <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">
          {tab === "baselines" ? (
            <>
              <p className="mb-3 text-[13px]" style={{ color: "var(--muted)" }}>
                What each person is expected to deliver in a <strong style={{ color: "var(--ink)" }}>full</strong>{" "}
                sprint, with nobody away. Set once; it carries into every sprint until you change it.
              </p>

              <div
                className="mb-4 rounded-lg px-3 py-2 text-[12.5px]"
                style={{ background: "var(--surface-2)", color: "var(--muted)" }}
              >
                Your team&rsquo;s convention: <strong style={{ color: "var(--ink)" }}>1 point = {hoursPerPoint}h</strong>
                {daysPerPoint === 1 ? " = 1 working day" : ` = ${daysPerPoint.toFixed(2)} working days`}, and a
                sprint is <strong style={{ color: "var(--ink)" }}>{sprintDays} working days</strong>.
                {" "}A full-time person is therefore about {(sprintDays / daysPerPoint).toFixed(0)} points.
              </div>

              {unregistered > 0 && (
                <p className="mb-3 rounded-lg px-3 py-2 text-[12.5px]"
                   style={{ background: "var(--warn-bg)", color: "var(--warn)" }}>
                  {unregistered} {unregistered === 1 ? "person has" : "people have"} delivered work with no
                  baseline set, so there is nothing to compare it against.
                </p>
              )}

              <table className="w-full border-collapse">
                <thead>
                  <tr>
                    <th className="pb-2 text-left text-[11px] font-semibold tracking-wide uppercase" style={{ color: "var(--faint)" }}>Person</th>
                    <th className="pb-2 text-right text-[11px] font-semibold tracking-wide uppercase" style={{ color: "var(--faint)" }}>Baseline</th>
                    <th className="pb-2 text-right text-[11px] font-semibold tracking-wide uppercase" style={{ color: "var(--faint)" }}>Which is</th>
                  </tr>
                </thead>
                <tbody>
                  {report.people.map((p) => (
                    <tr key={p.account_id} className="border-t" style={{ borderColor: "var(--line)" }}>
                      <td className="py-2.5 pr-3 text-[13px]">
                        {p.name}
                        {!p.registered && (
                          <div className="text-[11px]" style={{ color: "var(--warn)" }}>not set</div>
                        )}
                      </td>
                      <td className="py-2.5 text-right">
                        <NumberField value={p.baseline} onCommit={(n) => setBaseline(p, n)} />
                        <span className="ml-1 text-[11px]" style={{ color: "var(--faint)" }}>pts</span>
                      </td>
                      <td className="py-2.5 text-right text-[12px]" style={{ color: "var(--muted)" }}>
                        {p.baseline > 0
                          ? `${(p.baseline * daysPerPoint).toFixed(1)} days · ${(p.baseline * hoursPerPoint).toFixed(0)}h`
                          : "—"}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </>
          ) : (
            <>
              <p className="mb-3 text-[13px]" style={{ color: "var(--muted)" }}>
                Who was away during sprint {report.sprint.number}, in working days. Capacity is the
                baseline less that time — so this is the number delivery is actually measured against.
              </p>

              <div
                className="mb-4 rounded-lg px-3 py-2 text-[12.5px]"
                style={{ background: "var(--surface-2)", color: "var(--muted)" }}
              >
                <strong style={{ color: "var(--ink)" }}>Planned</strong> was known before the sprint began —
                booked leave, a public holiday, agreed part-time days.{" "}
                <strong style={{ color: "var(--ink)" }}>Unplanned</strong> came up during it, and is what
                explains a shortfall nobody could have planned around. The reason is deliberately not recorded.
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
                  {report.people.map((p) => (
                    <tr key={p.account_id} className="border-t" style={{ borderColor: "var(--line)" }}>
                      <td className="py-2.5 pr-3 text-[13px]">
                        {p.name}
                        {!p.registered && (
                          <div className="text-[11px]" style={{ color: "var(--warn)" }}>no baseline</div>
                        )}
                      </td>
                      <td className="py-2.5 text-right">
                        <NumberField value={p.planned_days_off} width="w-14"
                                     onCommit={(n) => setDays(p, n, p.unplanned_days_off)} />
                        <span className="ml-1 text-[11px]" style={{ color: "var(--faint)" }}>d</span>
                      </td>
                      <td className="py-2.5 text-right">
                        <NumberField value={p.unplanned_days_off} width="w-14"
                                     onCommit={(n) => setDays(p, p.planned_days_off, n)} />
                        <span className="ml-1 text-[11px]" style={{ color: "var(--faint)" }}>d</span>
                      </td>
                      <td className="tnum py-2.5 text-right text-[13px] font-semibold">
                        {p.registered ? `${p.capacity.toFixed(1)} pts` : "—"}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>

              <p className="mt-4 text-[12px]" style={{ color: "var(--faint)" }}>
                A day off costs baseline ÷ {sprintDays} points, so someone on 10 points losing 2 days
                has a capacity of {(10 - (10 / sprintDays) * 2).toFixed(1)}.
              </p>
            </>
          )}
        </div>
      </aside>
    </>
  );
}
