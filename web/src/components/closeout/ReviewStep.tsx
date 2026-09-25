import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import {
  applyChanges, fetchWritesStatus, proposeChanges, reverseChanges,
  type ApplyResult, type ChangeSet, type RowResult,
} from "../../api";
import { opLabel, type ChangeDraft } from "../../changes";
import { Badge, type Tone } from "../Badge";
import { PreviewBody, WritesOffNote } from "../ChangePreview";
import { Empty, StepIntro, filled, outlined, small, th } from "./Table";
import { WritesLog } from "./WritesLog";

/* Step 6: review and apply.
 *
 * The draft is posted for a preview when the step opens, the preview is
 * shown as the report page shows it, and under it sits the one button in
 * the tool that writes to Jira. The apply is sequential and guarded row
 * by row on the server; what comes back is one line per row in preview
 * order, so the table read before pressing the button is the table read
 * after it. A batch can then be reversed: that is a new preview, built
 * from the audit rows, applied like any other. */

function outcomeTone(o: RowResult["outcome"]): Tone {
  return o === "applied" ? "good" : o === "skipped" ? "warning" : "critical";
}

function Results({ r }: { r: ApplyResult }) {
  return (
    <div className="mt-5">
      <div className="mb-2 flex flex-wrap items-center gap-2">
        <Badge tone="good" label={`${r.applied} applied`} />
        {r.skipped > 0 && <Badge tone="warning" label={`${r.skipped} skipped`} />}
        {r.failed > 0 && <Badge tone="critical" label={`${r.failed} failed`} />}
      </div>
      {r.stopped && (
        <p className="mb-2 rounded-lg px-3 py-2 text-[13px]" style={{ background: "var(--warn-bg)", color: "var(--warn)" }}>
          Stopped early: {r.stopped}
        </p>
      )}
      <table className="w-full border-collapse text-[13px]">
        <thead>
          <tr>
            {["Key", "What", "To", "Outcome", "Reason"].map((h) => (
              <th key={h} className={th} style={{ color: "var(--faint)" }}>{h}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {r.rows.map((row, i) => (
            <tr key={i} className="border-t align-top" style={{ borderColor: "var(--line)" }}>
              <td className="py-2 pr-3 font-mono text-xs">{row.key}</td>
              <td className="py-2 pr-3 whitespace-nowrap">{opLabel(row.op)}</td>
              <td className="py-2 pr-3">
                {row.after_label}
                {row.person_label && <span style={{ color: "var(--muted)" }}> · {row.person_label}</span>}
              </td>
              <td className="py-2 pr-3"><Badge tone={outcomeTone(row.outcome)} label={row.outcome} /></td>
              <td className="py-2" style={{ color: "var(--muted)" }}>{row.reason}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

export function ReviewStep({
  sprintNumber, draft, onApplied,
}: {
  sprintNumber: number;
  draft: ChangeDraft;
  /** Fired after a batch lands so the report, the closeout and the draft
   *  are read again and every count on the rail moves. */
  onApplied: () => void;
}) {
  const requests = draft.list.map((q) => q.request);
  const [cs, setCs] = useState<ChangeSet | null>(null);
  const [result, setResult] = useState<ApplyResult | null>(null);
  const [reversed, setReversed] = useState(false);

  const preview = useMutation({
    mutationFn: () => proposeChanges(sprintNumber, requests),
    onSuccess: (data) => {
      setCs(data);
      setResult(null);
      setReversed(false);
    },
  });
  // Once, when the step opens with something to show. The ref keeps a
  // development-mode double effect from holding two change sets on the
  // server for one visit. Changing the draft afterwards means pressing
  // Preview again, which is offered beside the count.
  const asked = useRef(false);
  useEffect(() => {
    if (asked.current || requests.length === 0) return;
    asked.current = true;
    preview.mutate();
  }, [preview, requests.length]);

  const writes = useQuery({ queryKey: ["writes-status"], queryFn: fetchWritesStatus, staleTime: 60_000 });
  const apply = useMutation({
    mutationFn: () => applyChanges(cs!.id, cs!.digest),
    onSuccess: (r) => {
      setResult(r);
      onApplied();
    },
  });
  const reverse = useMutation({
    mutationFn: () => reverseChanges(cs!.id),
    onSuccess: (data) => {
      setCs(data);
      setResult(null);
      setReversed(true);
    },
  });

  const count = cs?.changes.length ?? 0;
  const expired = cs ? new Date(cs.expires_at).getTime() <= Date.now() : false;
  const allowed = writes.data?.allowed ?? false;
  const canApply = allowed && !!cs && count > 0 && !result && !apply.isPending && !expired;
  const err = (m: { error: unknown }) => (m.error instanceof Error ? m.error.message : m.error ? String(m.error) : "");

  return (
    <>
      <StepIntro title="Review and apply">
        What the server would write, and what it would not. Applying goes row by row, checking
        each field is still as previewed immediately before writing it, and stops on repeated
        refusals. Every attempt leaves an audit row; a batch can be reversed from those rows.
      </StepIntro>

      {requests.length === 0 && !cs && (
        <Empty>Nothing is queued. Accept a row in steps 2 to 5, or type into a flag on the report.</Empty>
      )}

      {requests.length > 0 && (
        <div className="mb-3 flex flex-wrap items-center gap-2 text-[12.5px]" style={{ color: "var(--muted)" }}>
          <span>{requests.length} queued in the draft.</span>
          <button onClick={() => preview.mutate()} disabled={preview.isPending} className={small} style={outlined}>
            {cs ? "Preview again" : "Preview"}
          </button>
          {draft.saving === "pending" && <span style={{ color: "var(--faint)" }}>saving the draft…</span>}
        </div>
      )}

      {preview.isPending && <p className="text-[13px]" style={{ color: "var(--muted)" }}>Asking the server what it would do…</p>}
      {preview.error && (
        <p className="rounded-lg px-3 py-2 text-[13px]" style={{ background: "var(--crit-bg)", color: "var(--crit)" }}>{err(preview)}</p>
      )}

      {cs && (
        <>
          {reversed && (
            <p className="mb-3 rounded-lg px-3 py-2 text-[13px]" style={{ background: "var(--info-bg)", color: "var(--info)" }}>
              This is the reverse of the batch just applied. Applying it puts the fields back.
            </p>
          )}
          <PreviewBody cs={cs} />

          <div
            className="mt-5 flex flex-wrap items-center justify-between gap-2 rounded-lg px-3 py-2.5"
            style={{ background: "var(--surface-2)" }}
          >
            {writes.data && !allowed ? (
              <WritesOffNote setting={writes.data.setting} />
            ) : (
              <span className="text-[12.5px]" style={{ color: expired ? "var(--warn)" : "var(--muted)" }}>
                {result
                  ? "Applied. The report is being rebuilt."
                  : expired
                    ? "This preview has expired. Preview again before applying."
                    : count === 0
                      ? "Nothing to write."
                      : `Writes ${count} ${count === 1 ? "change" : "changes"} to Jira as ${cs.actor || "the configured account"}, one at a time.`}
              </span>
            )}
            <span className="flex items-center gap-2">
              {result && result.applied > 0 && (
                <button
                  onClick={() => reverse.mutate()}
                  disabled={reverse.isPending}
                  className="rounded-lg px-3 py-1.5 text-sm font-medium disabled:opacity-40"
                  style={{ background: "var(--surface)", border: "1px solid var(--line)", color: "var(--ink)" }}
                >
                  {reverse.isPending ? "Building the reverse…" : "Reverse this batch"}
                </button>
              )}
              {!result && (
                <button
                  onClick={() => apply.mutate()}
                  disabled={!canApply}
                  className="rounded-lg px-3 py-1.5 text-sm font-medium disabled:opacity-40"
                  style={filled}
                >
                  {apply.isPending ? "Writing…" : `Write ${count} ${count === 1 ? "change" : "changes"} to Jira`}
                </button>
              )}
            </span>
          </div>

          {apply.error && (
            <p className="mt-2 rounded-lg px-3 py-2 text-[13px]" style={{ background: "var(--crit-bg)", color: "var(--crit)" }}>{err(apply)}</p>
          )}
          {reverse.error && (
            <p className="mt-2 rounded-lg px-3 py-2 text-[13px]" style={{ background: "var(--crit-bg)", color: "var(--crit)" }}>{err(reverse)}</p>
          )}
          {result && <Results r={result} />}
        </>
      )}

      <WritesLog />
    </>
  );
}
