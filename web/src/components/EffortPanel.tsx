import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { fetchWorklog, type SprintRow, type StoredPerson, type WorklogView } from "../api";
import { describe, figure, hoursLabel, nameOf, type ChangeDraft, type Queued } from "../changes";
import { Badge } from "./Badge";
import { Icon } from "./Icons";
import { parse } from "./Panel";
import { BlankNumberField, PersonSelect } from "./FlagInputs";
import { formatDay } from "../dates";

/* Effort on a carried ticket.
 *
 * Carryover is the one place time is logged from here, because it is the
 * one place it is needed: a ticket still open when the sprint closed is
 * credited only for what was worked on in it, and that credit comes from
 * the worklog. The disclosure shows what is logged now and takes what
 * should be, in hours, with the points shown beside them. Nothing typed
 * here is sent until Preview; the requests queue in the draft and are
 * listed inline so a person can see what they have asked for. */

const small = "rounded px-2 py-0.5 text-[12px] disabled:opacity-50";
const outlined = { color: "var(--muted)", border: "1px solid var(--line)" } as const;

/** Effort wraps one Carryover row: the row itself, the disclosure button
 *  beside it, and the panel underneath when open. */
export function Effort({
  row, sprintNumber, roster, hoursPerPoint, draft, children,
}: {
  row: SprintRow;
  sprintNumber: number;
  roster: StoredPerson[];
  hoursPerPoint?: number;
  draft: ChangeDraft;
  children: React.ReactNode;
}) {
  const [open, setOpen] = useState(false);
  const queued = draft.forKey(row.key).length;
  return (
    <>
      <div className="flex items-baseline gap-2">
        {children}
        {queued > 0 && <Badge tone="info" label={`${queued} queued`} />}
        <button
          onClick={() => setOpen(!open)}
          aria-expanded={open}
          className="flex shrink-0 items-center gap-1 rounded px-2 py-0.5 text-[12px]"
          style={outlined}
        >
          Effort
          <span style={{ display: "inline-flex", transform: open ? "rotate(-90deg)" : "rotate(180deg)" }}>
            <Icon.chevron />
          </span>
        </button>
      </div>
      {open && (
        <EffortBody row={row} sprintNumber={sprintNumber} roster={roster} hoursPerPoint={hoursPerPoint} draft={draft} />
      )}
    </>
  );
}

/** Who an entry credits, as a person would say it. The comment's
 *  @mentions are the attribution; an entry naming nobody is its
 *  author's own time. Several names is a split, and it is badged because
 *  it is the case the report cannot always read cleanly. */
function whoOf(e: WorklogView, roster: StoredPerson[]) {
  if (e.people.length === 0) return { names: e.author_label || nameOf(roster, e.author), split: false };
  const labels = e.people.map((p) => (p.label === p.id ? nameOf(roster, p.id) : p.label));
  return { names: labels.join(", "), split: e.people.length > 1 };
}

/** The one person a correction can be for: the single mention, or the
 *  author of an entry mentioning nobody. A split entry has no such
 *  person, and is corrected by adding one entry per person and removing
 *  it, which is the invariant the whole flow exists for. */
function onlyPerson(e: WorklogView): string | undefined {
  if (e.people.length === 1) return e.people[0].id;
  if (e.people.length === 0) return e.author || undefined;
  return undefined;
}

function Entry({
  e, issueKey, roster, hoursPerPoint, draft,
}: {
  e: WorklogView;
  issueKey: string;
  roster: StoredPerson[];
  hoursPerPoint?: number;
  draft: ChangeDraft;
}) {
  const [editing, setEditing] = useState(false);
  const [hours, setHours] = useState("");
  const who = whoOf(e, roster);
  const person = onlyPerson(e);
  const onRoster = roster.some((p) => p.account_id === person && p.active);
  const queued = draft.list.find((q) => q.request.key === issueKey && q.request.worklog_id === e.id);

  const queueHours = () => {
    const n = parse(hours);
    if (Number.isNaN(n) || n <= 0 || !person) return;
    // The note travels with the correction so the text beside the name
    // survives it; only the number changes.
    draft.add({ key: issueKey, op: "worklog.update", worklog_id: e.id, person, hours: n, note: e.note });
    setEditing(false);
    setHours("");
  };

  return (
    <li className="flex flex-wrap items-center gap-2 py-1.5 text-[13px]">
      <span>{who.names}</span>
      {who.split && <Badge tone="info" label="split" title="One entry naming several people. Add one entry per person, then remove this one." />}
      <span className="tnum" style={{ color: "var(--muted)" }}>{figure(e.hours)}h</span>
      <span className="text-[12px]" style={{ color: "var(--faint)" }}>{formatDay(e.started)}</span>
      {e.note && <span className="truncate text-[12px]" style={{ color: "var(--muted)" }}>{e.note}</span>}
      <span className="ml-auto flex shrink-0 items-center gap-2">
        {queued ? (
          <>
            <Badge tone="warning" label={describe(queued.request, roster, hoursPerPoint)} />
            <button onClick={() => draft.remove(queued.id)} className={small} style={outlined}>Undo</button>
          </>
        ) : editing ? (
          <>
            <BlankNumberField label={`New hours for entry ${e.id}`} placeholder="h" value={hours} onChange={setHours} width="w-14" />
            <span className="tnum text-[12px]" style={{ color: "var(--faint)" }}>
              {!Number.isNaN(parse(hours)) && hoursLabel(parse(hours), hoursPerPoint)}
            </span>
            <button onClick={queueHours} disabled={Number.isNaN(parse(hours)) || parse(hours) <= 0} className={`${small} font-medium`} style={{ background: "var(--accent)", color: "#fff" }}>Queue</button>
            <button onClick={() => setEditing(false)} className={small} style={outlined}>Cancel</button>
          </>
        ) : (
          <>
            {person && onRoster && (
              <button onClick={() => setEditing(true)} className={small} style={outlined}>Change hours</button>
            )}
            <button
              onClick={() => draft.add({ key: issueKey, op: "worklog.delete", worklog_id: e.id })}
              className={small}
              style={outlined}
            >
              Remove
            </button>
          </>
        )}
      </span>
    </li>
  );
}

function EffortBody({
  row, sprintNumber, roster, hoursPerPoint, draft,
}: {
  row: SprintRow;
  sprintNumber: number;
  roster: StoredPerson[];
  hoursPerPoint?: number;
  draft: ChangeDraft;
}) {
  const log = useQuery({
    queryKey: ["worklog", sprintNumber, row.key],
    queryFn: () => fetchWorklog(sprintNumber, row.key),
  });
  const [person, setPerson] = useState("");
  const [hours, setHours] = useState("");
  const [note, setNote] = useState("");
  const n = parse(hours);
  const valid = person !== "" && !Number.isNaN(n) && n > 0;

  const add = () => {
    if (!valid) return;
    draft.add({ key: row.key, op: "worklog.add", person, hours: n, note: note.trim() || undefined });
    setHours("");
    setNote("");
  };

  const adds: Queued[] = draft.forKey(row.key).filter((q) => q.request.op === "worklog.add");

  return (
    <div className="mt-2 rounded-lg px-3 py-2" style={{ background: "var(--surface-2)" }}>
      {log.isLoading && <p className="py-1 text-[12.5px]" style={{ color: "var(--muted)" }}>Reading what is logged…</p>}
      {log.error && (
        <p className="py-1 text-[12.5px]" style={{ color: "var(--crit)" }}>{String(log.error)}</p>
      )}
      {log.data && log.data.entries.length === 0 && (
        <p className="py-1 text-[12.5px]" style={{ color: "var(--muted)" }}>Nothing logged on this ticket yet.</p>
      )}
      {log.data && log.data.entries.length > 0 && (
        <ul className="divide-y" style={{ borderColor: "var(--line)" }}>
          {log.data.entries.map((e) => (
            <Entry key={e.id} e={e} issueKey={row.key} roster={roster} hoursPerPoint={hoursPerPoint} draft={draft} />
          ))}
        </ul>
      )}

      {adds.length > 0 && (
        <ul className="mt-2 space-y-1">
          {adds.map((q) => (
            <li key={q.id} className="flex items-center gap-2 text-[12.5px]">
              <Badge tone="warning" label="queued" />
              <span>{describe(q.request, roster, hoursPerPoint)}</span>
              {q.request.note && <span style={{ color: "var(--muted)" }}>· {q.request.note}</span>}
              <button onClick={() => draft.remove(q.id)} className={`${small} ml-auto`} style={outlined}>Undo</button>
            </li>
          ))}
        </ul>
      )}

      {/* The form. Hours in, points shown: a person recalling "about a
          day" types 6 and sees what the report will make of it. The
          date is left to the server, which uses the sprint's close. */}
      <div className="mt-2 flex flex-wrap items-center gap-2 border-t pt-2" style={{ borderColor: "var(--line)" }}>
        <PersonSelect roster={roster} label={`Person to log time for on ${row.key}`} value={person} onChange={setPerson} />
        <BlankNumberField label={`Hours on ${row.key}`} placeholder="hours" value={hours} onChange={setHours} width="w-20" />
        <span className="tnum text-[12px]" style={{ color: "var(--faint)" }}>
          {!Number.isNaN(n) && n > 0 ? hoursLabel(n, hoursPerPoint) : "hours"}
        </span>
        <input
          type="text"
          aria-label={`Note for the entry on ${row.key}`}
          placeholder="note, optional"
          value={note}
          onChange={(e) => setNote(e.target.value)}
          className="min-w-32 flex-1 rounded px-2 py-1 text-[13px] outline-none"
          style={{ background: "var(--surface)", border: "1px solid var(--line)", color: "var(--ink)" }}
        />
        <button
          onClick={add}
          disabled={!valid}
          className="rounded px-2.5 py-1 text-[12.5px] font-medium disabled:opacity-40"
          style={{ background: "var(--accent)", color: "#fff" }}
        >
          Add
        </button>
      </div>
    </div>
  );
}
