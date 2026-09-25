import { useState } from "react";
import type { CloseoutSizeRow } from "../../api";
import { figure, type ChangeDraft } from "../../changes";
import { BlankNumberField } from "../FlagInputs";
import { parse } from "../Panel";
import { Empty, KeyCell, RowActions, Source, StepIntro, StepTable, type Skips } from "./Table";

/* Step 2: size what finished.
 *
 * A finished Task or Bug with no points is credited nothing, so the
 * sprint reads short by exactly the work nobody sized. The refinement
 * estimate, where one was recorded, is the pre-filled suggestion: the
 * report has been using it in place of the actual anyway, and writing
 * it down turns a stand-in into a value somebody chose. */

export const sizeSkipID = (key: string) => `size:${key}`;

function Row({ row, draft, skips }: { row: CloseoutSizeRow; draft: ChangeDraft; skips: Skips }) {
  // What has been typed, kept as text so a half-typed "2." is not
  // rounded away under the person's cursor.
  const [typed, setTyped] = useState("");
  const id = sizeSkipID(row.key);
  const queued = draft.find(row.key, "points.set");
  const skipped = skips.has(id);

  // The queued figure where there is one, otherwise what was typed,
  // otherwise the suggestion: the request is the truth and the field
  // is its echo.
  const shown = queued
    ? (typed !== "" ? typed : figure(queued.request.points ?? 0))
    : typed !== "" ? typed : row.suggested != null ? figure(row.suggested) : "";
  const n = parse(shown);
  const valid = !Number.isNaN(n) && n > 0;

  const onChange = (s: string) => {
    setTyped(s);
    // A queued row is edited in place; clearing it drops the request.
    if (queued) {
      const v = parse(s);
      draft.set(row.key, "points.set", Number.isNaN(v) || v <= 0 ? null : { key: row.key, op: "points.set", points: v });
    }
  };

  return (
    <tr className="border-t align-middle" style={{ borderColor: "var(--line)" }}>
      <KeyCell issueKey={row.key} url={row.url} summary={row.summary} />
      <td className="py-2 pr-3 whitespace-nowrap" style={{ color: "var(--muted)" }}>{row.type}</td>
      <td className="py-2 pr-3 whitespace-nowrap" style={{ color: "var(--muted)" }}>{row.status}</td>
      <td className="py-2 pr-3">
        {skipped ? (
          <span style={{ color: "var(--faint)" }}>—</span>
        ) : (
          <BlankNumberField label={`Story points for ${row.key}`} placeholder="pts" value={shown} onChange={onChange} />
        )}
      </td>
      <td className="py-2 pr-3">
        {row.suggested != null ? (
          <Source label="estimate" title="The refinement estimate on the ticket. The report has been using it in place of the missing actual." />
        ) : (
          <Source label="no estimate" title="Refinement recorded nothing either. Type what the work was worth." />
        )}
      </td>
      <td className="py-2 text-right">
        <RowActions
          queued={!!queued}
          skipped={skipped}
          canAccept={valid}
          onAccept={() => draft.set(row.key, "points.set", { key: row.key, op: "points.set", points: n })}
          onSkip={() => {
            draft.set(row.key, "points.set", null);
            setTyped("");
            skips.set(id, true);
          }}
          onUndo={() => skips.set(id, false)}
        />
      </td>
    </tr>
  );
}

export function SizeStep({ rows, draft, skips }: { rows: CloseoutSizeRow[]; draft: ChangeDraft; skips: Skips }) {
  return (
    <>
      <StepIntro title="Size what finished">
        Tasks and Bugs that finished in this sprint with no story points. Unsized work is credited
        nothing, so each of these is a hole in the delivered total. The estimate is offered where
        refinement recorded one; accept it, type the actual, or skip the ticket.
      </StepIntro>
      {rows.length === 0 ? (
        <Empty>Everything that finished has points.</Empty>
      ) : (
        <StepTable headers={["Key", "Type", "Status", "Points", "From", ""]}>
          {rows.map((r) => <Row key={r.key} row={r} draft={draft} skips={skips} />)}
        </StepTable>
      )}
    </>
  );
}
