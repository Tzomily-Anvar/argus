import { useState } from "react";
import type { CloseoutStoryRow } from "../../api";
import { figure, type ChangeDraft } from "../../changes";
import { Badge } from "../Badge";
import { BlankNumberField } from "../FlagInputs";
import { parse } from "../Panel";
import { Empty, KeyCell, RowActions, Source, StepIntro, StepTable, filled, outlined, small, type Skips } from "./Table";

/* Step 5: stories wrapping up.
 *
 * A Story's points are a rollup of the work beneath it, never delivery
 * in their own right, and the one moment they can be written honestly is
 * when that work is all done and the sum is known. Where the Story
 * already carries a figure this is the one overwrite the write-back
 * allows, and it is confirmed by hand: the guard carries the current
 * value and the apply writes only where Jira still holds it. */

export const storySkipID = (key: string) => `story:${key}`;

function Row({ row, draft, skips }: { row: CloseoutStoryRow; draft: ChangeDraft; skips: Skips }) {
  const [typed, setTyped] = useState("");
  const [confirming, setConfirming] = useState(false);
  const id = storySkipID(row.key);
  const queued = draft.find(row.key, "points.set");
  const skipped = skips.has(id);

  const shown = queued
    ? (typed !== "" ? typed : figure(queued.request.points ?? 0))
    : typed !== "" ? typed : row.suggested != null ? figure(row.suggested) : "";
  const n = parse(shown);
  const valid = !Number.isNaN(n) && n > 0;
  const matches = row.sum_known && row.own_points != null && row.own_points === row.linked_points;
  const overwrite = row.own_points != null && row.own_points > 0;

  const onChange = (s: string) => {
    setTyped(s);
    setConfirming(false);
    if (queued) {
      const v = parse(s);
      draft.set(row.key, "points.set", Number.isNaN(v) || v <= 0 ? null : { key: row.key, op: "points.set", points: v });
    }
  };
  const queue = () => {
    draft.set(row.key, "points.set", { key: row.key, op: "points.set", points: n });
    setConfirming(false);
  };

  return (
    <tr className="border-t align-middle" style={{ borderColor: "var(--line)" }}>
      <KeyCell issueKey={row.key} url={row.url} summary={row.summary} />
      <td className="tnum py-2 pr-3 whitespace-nowrap" style={{ color: "var(--muted)" }}>
        {row.own_points == null ? "—" : figure(row.own_points)}
      </td>
      <td className="tnum py-2 pr-3 whitespace-nowrap">
        {row.sum_known ? figure(row.linked_points) : <Badge tone="warning" label="sum unknown" title="Some of the work beneath is unsized, so there is no sum to write. Size it first." />}
        <span className="ml-1 text-[11.5px]" style={{ color: "var(--faint)" }}>of {row.linked_count}</span>
      </td>
      <td className="py-2 pr-3">
        {!row.sum_known || skipped || matches ? (
          <span style={{ color: "var(--faint)" }}>—</span>
        ) : (
          <BlankNumberField label={`Story points for ${row.key}`} placeholder="pts" value={shown} onChange={onChange} />
        )}
      </td>
      <td className="py-2 pr-3">
        {row.sum_known && <Source label={`sum of ${row.linked_count}`} title="The points of the linked Tasks and Bugs, added up." />}
      </td>
      <td className="py-2 text-right">
        {!row.sum_known ? null : matches ? (
          <Badge tone="good" label="already matches" title="The Story's own points equal the sum beneath it." />
        ) : confirming ? (
          <span className="flex items-center justify-end gap-2 text-[12.5px]" style={{ color: "var(--warn)" }}>
            Replace {figure(row.own_points ?? 0)} with {figure(n)}?
            <button onClick={queue} className={`${small} font-medium`} style={filled}>Replace</button>
            <button onClick={() => setConfirming(false)} className={small} style={outlined}>Cancel</button>
          </span>
        ) : (
          <RowActions
            queued={!!queued}
            skipped={skipped}
            canAccept={valid}
            acceptLabel={overwrite ? "Replace…" : "Accept"}
            // An overwrite asks once more; a fill of an empty field does
            // not, because there is nothing to lose.
            onAccept={() => (overwrite ? setConfirming(true) : queue())}
            onSkip={() => {
              draft.set(row.key, "points.set", null);
              setTyped("");
              skips.set(id, true);
            }}
            onUndo={() => skips.set(id, false)}
          />
        )}
      </td>
    </tr>
  );
}

export function StoriesStep({ rows, draft, skips }: { rows: CloseoutStoryRow[]; draft: ChangeDraft; skips: Skips }) {
  return (
    <>
      <StepIntro title="Stories wrapping up">
        Stories whose linked work is all done. Their points are a rollup of that work, so the sum
        beneath is offered, with what the Story says about itself beside it. Where the two differ
        and the Story already has a figure, replacing it is confirmed here and again by the apply,
        which writes only if Jira still holds the value shown.
      </StepIntro>
      {rows.length === 0 ? (
        <Empty>No Story wrapped up this sprint.</Empty>
      ) : (
        <StepTable headers={["Key", "Own", "Linked", "Points", "From", ""]}>
          {rows.map((r) => <Row key={r.key} row={r} draft={draft} skips={skips} />)}
        </StepTable>
      )}
    </>
  );
}
