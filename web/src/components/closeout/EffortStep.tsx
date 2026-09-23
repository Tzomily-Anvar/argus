import { useState } from "react";
import type { CloseoutEffortRow, StoredPerson } from "../../api";
import { describe, figure, hoursLabel, type ChangeDraft } from "../../changes";
import { Badge } from "../Badge";
import { BlankNumberField, PersonSelect } from "../FlagInputs";
import { parse } from "../Panel";
import { formatDay } from "../../dates";
import { Empty, KeyCell, Source, StepIntro, StepTable, filled, outlined, small, type Skips } from "./Table";

/* Step 4: effort on carryover.
 *
 * A ticket still open at the close is credited only for what was worked
 * on in the sprint, and that credit comes from the worklog. The assignee
 * is the pre-filled person; the hours are always blank, because nobody
 * but the person knows them and a number typed here that nobody recalled
 * would be the one thing the change set must never contain. */

export const effortSkipID = (key: string) => `effort:${key}`;

const isWorklog = (op: string) => op.startsWith("worklog.");

function Row({
  row, roster, hoursPerPoint, draft, skips,
}: {
  row: CloseoutEffortRow;
  roster: StoredPerson[];
  hoursPerPoint?: number;
  draft: ChangeDraft;
  skips: Skips;
}) {
  const id = effortSkipID(row.key);
  const onRoster = roster.some((p) => p.account_id === row.assignee_account_id && p.active);
  const [person, setPerson] = useState(onRoster ? row.assignee_account_id : "");
  const [hours, setHours] = useState("");
  const [note, setNote] = useState("");
  const skipped = skips.has(id);
  const queued = draft.forKey(row.key).filter((q) => isWorklog(q.request.op));

  const n = parse(hours);
  const valid = person !== "" && !Number.isNaN(n) && n > 0;
  const add = () => {
    if (!valid) return;
    draft.add({ key: row.key, op: "worklog.add", person, hours: n, note: note.trim() || undefined });
    setHours("");
    setNote("");
  };

  return (
    <tr className="border-t align-top" style={{ borderColor: "var(--line)" }}>
      <KeyCell issueKey={row.key} url={row.url} summary={row.summary} />
      <td className="py-2 pr-3 whitespace-nowrap" style={{ color: "var(--muted)" }}>
        {/* Where it stood at the close is why it is here; where it stands
            now is a different fact, and a ticket finished since still
            earned this sprint whatever was spent on it here. */}
        {row.status_at_close}
        {row.status !== row.status_at_close && (
          <div className="text-[11px]" style={{ color: "var(--faint)" }}>now {row.status}</div>
        )}
      </td>
      <td className="py-2 pr-3">
        <div className="tnum whitespace-nowrap">
          {figure(row.hours_logged)}h
          <span className="ml-1 text-[11px]" style={{ color: "var(--faint)" }}>in this sprint</span>
        </div>
        {/* Every entry on the ticket, so what is typed is added to a known
            figure. Only the ones inside the window count here; the rest
            are shown greyed and said to be before or after, so a 0h above
            a 2h line does not read as a contradiction. */}
        {row.entries.length > 0 && (
          <ul className="mt-0.5 space-y-0.5 text-[11.5px]" style={{ color: "var(--faint)" }}>
            {row.entries.map((e) => (
              <li key={e.id} className="whitespace-nowrap" style={{ opacity: e.window && e.window !== "inside" ? 0.6 : 1 }}>
                {e.people.length > 0 ? e.people.map((p) => p.label).join(", ") : e.author_label} · {figure(e.hours)}h · {formatDay(e.started, false)}
                {e.window === "before" && " · before the sprint"}
                {e.window === "after" && " · after the close"}
              </li>
            ))}
          </ul>
        )}
      </td>
      <td className="py-2 pr-3">
        {skipped ? (
          <span style={{ color: "var(--faint)" }}>nothing to log</span>
        ) : (
          <div className="flex flex-wrap items-center gap-2">
            <PersonSelect roster={roster} label={`Person to log time for on ${row.key}`} value={person} onChange={setPerson} />
            <BlankNumberField label={`Hours on ${row.key}`} placeholder="hours" value={hours} onChange={setHours} width="w-20" />
            <span className="tnum text-[12px]" style={{ color: "var(--faint)" }}>
              {valid ? hoursLabel(n, hoursPerPoint) : "hours"}
            </span>
            <input
              type="text"
              aria-label={`Note for the entry on ${row.key}`}
              placeholder="note, optional"
              value={note}
              onChange={(e) => setNote(e.target.value)}
              className="min-w-28 flex-1 rounded px-2 py-1 text-[13px] outline-none"
              style={{ background: "var(--surface-2)", border: "1px solid var(--line)", color: "var(--ink)" }}
            />
          </div>
        )}
        {queued.length > 0 && (
          <ul className="mt-2 space-y-1">
            {queued.map((q) => (
              <li key={q.id} className="flex items-center gap-2 text-[12.5px]">
                <Badge tone="info" label="queued" />
                <span>{describe(q.request, roster, hoursPerPoint)}</span>
                {q.request.note && <span style={{ color: "var(--muted)" }}>· {q.request.note}</span>}
                <button onClick={() => draft.remove(q.id)} className={`${small} ml-auto`} style={outlined}>Undo</button>
              </li>
            ))}
          </ul>
        )}
      </td>
      <td className="py-2 pr-3">
        {row.hours_logged > 0 && (
          <span className="mr-2">
            <Badge tone="good" label="logged" title="Time is already logged inside this sprint. Add more only if somebody else worked on it." />
          </span>
        )}
        {onRoster ? (
          <Source label="assignee" title={`${row.assignee_label} is assigned the ticket. The hours are left to you.`} />
        ) : row.assignee_account_id ? (
          <Source label="assignee not on roster" title={`${row.assignee_label} is assigned but is not opted in on the roster, so nobody is pre-filled.`} />
        ) : (
          <Source label="unassigned" title="Nobody is assigned, so nobody is pre-filled." />
        )}
      </td>
      <td className="py-2 text-right">
        <span className="flex items-center justify-end gap-2">
          {skipped ? (
            <>
              <Badge tone="neutral" label="skipped" title="Left alone this sitting. Nothing will be written." />
              <button onClick={() => skips.set(id, false)} className={small} style={outlined}>Undo</button>
            </>
          ) : (
            <>
              <button onClick={add} disabled={!valid} className={`${small} font-medium`} style={filled}>Queue</button>
              {queued.length === 0 && (
                <button onClick={() => skips.set(id, true)} className={small} style={outlined}>Skip</button>
              )}
            </>
          )}
        </span>
      </td>
    </tr>
  );
}

export function EffortStep({
  rows, roster, hoursPerPoint, draft, skips,
}: {
  rows: CloseoutEffortRow[];
  roster: StoredPerson[];
  hoursPerPoint?: number;
  draft: ChangeDraft;
  skips: Skips;
}) {
  return (
    <>
      <StepIntro title="Effort on carryover">
        Still open when the sprint closed, and worked on during it. This sprint is credited only for
        the time logged inside its window, so a ticket with nothing logged here earns it nothing,
        whatever sprint finishes it later. For each person who worked on a ticket this sprint, type
        their hours; the points show beside them and the entry is dated at the close. Rows with time
        already logged sit last. Corrections to existing entries are made from the Effort disclosure
        on the report's Carryover rows.
      </StepIntro>
      {rows.length === 0 ? (
        <Empty>Nothing was carried over with work on it.</Empty>
      ) : (
        <StepTable headers={["Key", "Status", "Logged", "Add", "From", ""]}>
          {[...rows]
            .sort((a, b) => Number(a.hours_logged > 0) - Number(b.hours_logged > 0) || a.key.localeCompare(b.key))
            .map((r) => (
              <Row key={r.key} row={r} roster={roster} hoursPerPoint={hoursPerPoint} draft={draft} skips={skips} />
            ))}
        </StepTable>
      )}
    </>
  );
}
