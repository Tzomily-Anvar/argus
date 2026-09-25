import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { saveCapacity, type CloseoutPerson, type SprintPerson, type SprintReport } from "../../api";
import { Badge } from "../Badge";
import { stateOf } from "../Panel";
import { PublishStep } from "./PublishStep";
import { Empty, StepIntro, th } from "./Table";

/* Step 7: reasons and publish.
 *
 * One line per measured person, saying in a sentence why the number is
 * what it is. The line is the capacity note, which already persists
 * through the same PUT the Capacity panel uses; saving it sends the
 * person's days off along unchanged, because that endpoint replaces the
 * whole row. It saves on blur rather than through a Save button, and
 * the field keeps what was typed while the write is in flight so it
 * cannot appear to change its mind. Beneath the table sits the publish,
 * which builds the page from what is saved here; it is its own file
 * because it has its own preview and its own button. */

function NoteRow({ p, stored, sprintJiraID, onSaved }: { p: SprintPerson; stored: string; sprintJiraID: number; onSaved: () => void }) {
  const [typed, setTyped] = useState<string | null>(null);
  const save = useMutation({
    mutationFn: (note: string) =>
      saveCapacity({
        sprint_jira_id: sprintJiraID,
        account_id: p.account_id,
        planned_days_off: p.planned_days_off,
        unplanned_days_off: p.unplanned_days_off,
        reviewed: true,
        note: note || undefined,
      }),
    onSuccess: onSaved,
  });
  const state = stateOf(save);
  const value = typed ?? stored;

  const blur = () => {
    if (typed === null || typed.trim() === stored.trim()) return;
    save.mutate(typed.trim());
  };

  return (
    <tr className="border-t align-middle" style={{ borderColor: "var(--line)" }}>
      <td className="py-2 pr-3 whitespace-nowrap">{p.name}</td>
      <td
        className="tnum py-2 pr-3 text-right whitespace-nowrap"
        style={{ color: p.delta < 0 ? "var(--warn)" : "var(--good)" }}
      >
        {(p.delta > 0 ? "+" : "") + p.delta.toFixed(1)}
        {p.shortfall_from_absence > 0 && (
          <div className="text-[11px] font-normal" style={{ color: "var(--faint)" }}>
            {p.shortfall_from_absence.toFixed(1)} unplanned
          </div>
        )}
      </td>
      <td className="w-full py-2 pr-3">
        <input
          type="text"
          aria-label={`Reason for ${p.name}`}
          placeholder="why the figure is what it is, in a sentence"
          value={value}
          onChange={(e) => setTyped(e.target.value)}
          onBlur={blur}
          className="w-full rounded px-2 py-1 text-[13px] outline-none"
          style={{
            background: "var(--surface-2)",
            border: `1px solid ${typed !== null && typed.trim() !== stored.trim() ? "var(--accent)" : "var(--line)"}`,
            color: "var(--ink)",
          }}
        />
      </td>
      <td className="py-2 whitespace-nowrap">
        {state === "pending" && <Badge tone="neutral" label="saving…" />}
        {state === "saved" && <Badge tone="good" label="saved" />}
        {state === "error" && (
          <Badge tone="critical" label="not saved" title={save.error instanceof Error ? save.error.message : "Could not save"} />
        )}
      </td>
    </tr>
  );
}

export function ReasonsStep({
  report, people, onSaved,
}: {
  report: SprintReport;
  people: CloseoutPerson[];
  onSaved: () => void;
}) {
  const measured = report.people.filter((p) => p.measured);
  const noteOf = (p: SprintPerson) => people.find((c) => c.account_id === p.account_id)?.note ?? p.note ?? "";

  return (
    <>
      <StepIntro title="Reasons and publish">
        One line per measured person: why the figure is what it is. Saved as you leave the field,
        to the same place the Capacity panel keeps it. The page published below is built from what
        is saved here, with each reason beside the number it explains.
      </StepIntro>

      {measured.length === 0 ? (
        <Empty>Nobody is being measured this sprint, so there is nothing to explain.</Empty>
      ) : (
        <table className="w-full border-collapse text-[13px]">
          <thead>
            <tr>
              <th className={th} style={{ color: "var(--faint)" }}>Person</th>
              <th className={`${th} text-right`} style={{ color: "var(--faint)" }}>Delta</th>
              <th className={th} style={{ color: "var(--faint)" }}>Reason</th>
              <th className={th} style={{ color: "var(--faint)" }}></th>
            </tr>
          </thead>
          <tbody>
            {measured.map((p) => (
              <NoteRow key={p.account_id} p={p} stored={noteOf(p)} sprintJiraID={report.sprint.jira_id} onSaved={onSaved} />
            ))}
          </tbody>
        </table>
      )}

      <PublishStep sprintNumber={report.sprint.number} />
    </>
  );
}
