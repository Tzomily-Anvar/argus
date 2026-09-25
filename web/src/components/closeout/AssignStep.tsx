import { useState } from "react";
import type { CloseoutAssignRow } from "../../api";
import type { ChangeDraft } from "../../changes";
import { Empty, KeyCell, RowActions, Source, StepIntro, StepTable, type Skips } from "./Table";

/* Step 3: assign what finished.
 *
 * Work finished with nobody assigned is delivered by nobody, and the per
 * person table has a line of unattributed points at the bottom for it.
 * The suggestion is whoever moved the ticket to Done, read from the
 * changelog - usually the person who did the work, sometimes the person
 * who tidied the board, which is why "leave it" is always offered. */

export const assignSkipID = (key: string) => `assign:${key}`;

function Row({ row, draft, skips }: { row: CloseoutAssignRow; draft: ChangeDraft; skips: Skips }) {
  const [chosen, setChosen] = useState<string | null>(null);
  const id = assignSkipID(row.key);
  const queued = draft.find(row.key, "assignee.set");
  const skipped = skips.has(id);

  // The suggestion is always among the options, even where the person
  // is not on the candidate list, so what is pre-filled can be seen.
  const options = row.candidates.some((c) => c.account_id === row.suggested_account_id) || !row.suggested_account_id
    ? row.candidates
    : [{ account_id: row.suggested_account_id, label: row.suggested_label }, ...row.candidates];

  const value = queued ? (queued.request.assignee ?? "") : chosen ?? row.suggested_account_id;

  const onChange = (v: string) => {
    setChosen(v);
    if (queued) draft.set(row.key, "assignee.set", v === "" ? null : { key: row.key, op: "assignee.set", assignee: v });
  };

  return (
    <tr className="border-t align-middle" style={{ borderColor: "var(--line)" }}>
      <KeyCell issueKey={row.key} url={row.url} summary={row.summary} />
      <td className="py-2 pr-3 whitespace-nowrap" style={{ color: "var(--muted)" }}>{row.type}</td>
      <td className="py-2 pr-3 whitespace-nowrap" style={{ color: "var(--muted)" }}>{row.status}</td>
      <td className="py-2 pr-3">
        {skipped ? (
          <span style={{ color: "var(--faint)" }}>left unassigned</span>
        ) : (
          <select
            aria-label={`Assignee for ${row.key}`}
            value={value}
            onChange={(e) => onChange(e.target.value)}
            className="rounded px-2 py-1 text-[13px] outline-none"
            style={{
              background: "var(--surface-2)",
              border: `1px solid ${value !== "" ? "var(--accent)" : "var(--line)"}`,
              color: value !== "" ? "var(--ink)" : "var(--muted)",
            }}
          >
            <option value="">— choose a person —</option>
            {options.map((c) => (
              <option key={c.account_id} value={c.account_id}>{c.label}</option>
            ))}
          </select>
        )}
      </td>
      <td className="py-2 pr-3">
        {row.suggested_account_id ? (
          <Source label="moved it to Done" title={`${row.suggested_label} moved the ticket to Done, according to its changelog.`} />
        ) : (
          <Source label="nobody recognisable" title="The changelog names nobody on the roster as having moved it to Done." />
        )}
      </td>
      <td className="py-2 text-right">
        <RowActions
          queued={!!queued}
          skipped={skipped}
          canAccept={value !== ""}
          skipLabel="Leave it"
          onAccept={() => draft.set(row.key, "assignee.set", { key: row.key, op: "assignee.set", assignee: value })}
          onSkip={() => {
            draft.set(row.key, "assignee.set", null);
            skips.set(id, true);
          }}
          onUndo={() => skips.set(id, false)}
        />
      </td>
    </tr>
  );
}

export function AssignStep({ rows, draft, skips }: { rows: CloseoutAssignRow[]; draft: ChangeDraft; skips: Skips }) {
  return (
    <>
      <StepIntro title="Assign what finished">
        Finished with nobody assigned, so the points count for the sprint and for no one. The
        person who moved each ticket to Done is offered; that is usually who did the work and
        sometimes who tidied the board, so leaving it is always an answer.
      </StepIntro>
      {rows.length === 0 ? (
        <Empty>Everything that finished has an assignee.</Empty>
      ) : (
        <StepTable headers={["Key", "Type", "Status", "Assignee", "From", ""]}>
          {rows.map((r) => <Row key={r.key} row={r} draft={draft} skips={skips} />)}
        </StepTable>
      )}
    </>
  );
}
