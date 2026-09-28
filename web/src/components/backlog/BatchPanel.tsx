import { useEffect, useRef, useState } from "react";
import { useMutation } from "@tanstack/react-query";
import {
  applyBatch, previewBatch, reverseBatch,
  type BatchAction, type BatchPreview, type BatchResult, type BatchRowResult,
} from "../../api";
import { Badge, type Tone } from "../Badge";
import { expiry, WritesOffNote } from "../ChangePreview";
import { filled, th } from "../closeout/Table";
import { SidePanel } from "../Panel";

/* The batch: preview, apply, results, reverse.
 *
 * Every bulk action goes through here, and this is the only place in
 * the backlog tool that writes to Jira. The request is posted for a
 * preview when the panel opens; the preview is a table of before and
 * after, one row per ticket, with the rows that would be left alone
 * saying why. Under it sits Apply, gated three ways: the writes switch,
 * the preview's expiry, and for a delete the second switch and a typed
 * count. What comes back is one line per ticket with its outcome, and
 * a batch that wrote something can be reversed - a new preview, built
 * from the audit rows, applied like any other - except a delete, which
 * has nothing to reverse from. */

/** What the bulk bar or a view asks for: the action, the tickets, the
 *  parameters the action takes, and a label saying it in words. */
export type BatchRequest = {
  action: BatchAction;
  keys: string[];
  params: Record<string, unknown>;
  label: string;
};

const WRITES = "ARGUS_SPRINT_ALLOW_WRITES";
const DELETES = "ARGUS_BACKLOG_ALLOW_DELETE";

function outcomeTone(o: BatchRowResult["outcome"]): Tone {
  return o === "applied" ? "good" : o === "skipped" ? "warning" : "critical";
}

function Preview({ cs }: { cs: BatchPreview }) {
  const rows = cs.rows ?? [];
  return (
    <>
      <div className="mb-3 flex flex-wrap items-center gap-2 text-[12.5px]" style={{ color: "var(--muted)" }}>
        <Badge tone={cs.changes > 0 ? "info" : "neutral"} label={`${cs.changes} ${cs.changes === 1 ? "change" : "changes"}`} />
        {cs.skipped > 0 && <Badge tone="warning" label={`${cs.skipped} skipped`} />}
        <span className="ml-auto">Valid until {expiry(cs.expires_at)}</span>
      </div>
      {rows.length === 0 ? (
        <p className="rounded-lg px-4 py-6 text-center text-[13px]" style={{ background: "var(--surface-2)", color: "var(--muted)" }}>
          Nothing would be written.
        </p>
      ) : (
        <table className="w-full border-collapse text-[13px]">
          <thead>
            <tr>
              {["Key", "From", "To", "Skipped because"].map((h) => (
                <th key={h} className={th} style={{ color: "var(--faint)" }}>{h}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => (
              <tr key={r.key} className="border-t align-top" style={{ borderColor: "var(--line)", opacity: r.skipped ? 0.7 : 1 }}>
                <td className="py-2 pr-3">
                  <span className="font-mono text-xs">{r.key}</span>
                  <div className="max-w-40 truncate text-[11.5px]" style={{ color: "var(--faint)" }} title={r.summary}>
                    {r.type ? `${r.type} · ` : ""}{r.summary}
                  </div>
                </td>
                <td className="py-2 pr-3" style={{ color: "var(--muted)" }}>{r.before || "(empty)"}</td>
                <td className="py-2 pr-3 font-medium">
                  {r.skipped ? <span style={{ color: "var(--faint)" }}>—</span> : <><span style={{ color: "var(--faint)" }}>→ </span>{r.after}</>}
                </td>
                <td className="py-2" style={{ color: "var(--muted)" }}>{r.skipped}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </>
  );
}

function Results({ r }: { r: BatchResult }) {
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
            {["Key", "Outcome", "Reason"].map((h) => (
              <th key={h} className={th} style={{ color: "var(--faint)" }}>{h}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {(r.rows ?? []).map((row, i) => (
            <tr key={i} className="border-t align-top" style={{ borderColor: "var(--line)" }}>
              <td className="py-2 pr-3 font-mono text-xs">{row.key}</td>
              <td className="py-2 pr-3"><Badge tone={outcomeTone(row.outcome)} label={row.outcome} /></td>
              <td className="py-2" style={{ color: "var(--muted)" }}>{row.reason}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

const errText = (e: unknown) => (e instanceof Error ? e.message : e ? String(e) : "");

export function BatchPanel({
  request, onClose, onApplied,
}: {
  request: BatchRequest;
  onClose: () => void;
  /** Fired after a batch lands, so the backlog is swept again and the
   *  selection it was made from is let go. */
  onApplied: () => void;
}) {
  const [cs, setCs] = useState<BatchPreview | null>(null);
  const [result, setResult] = useState<BatchResult | null>(null);
  const [reversed, setReversed] = useState(false);
  const [typed, setTyped] = useState("");

  const preview = useMutation({
    mutationFn: () => previewBatch(request.action, request.keys, request.params),
    onSuccess: (data) => { setCs(data); setResult(null); setReversed(false); },
  });
  // Once, on open. The ref keeps a development-mode double effect from
  // holding two previews on the server for one press.
  const asked = useRef(false);
  useEffect(() => {
    if (asked.current) return;
    asked.current = true;
    preview.mutate();
  }, [preview]);

  const isDelete = (cs?.action ?? request.action) === "issue.delete";
  const count = cs?.changes ?? 0;
  const expired = cs ? new Date(cs.expires_at).getTime() <= Date.now() : false;
  const confirmed = !isDelete || (count > 0 && typed.trim() === String(count));

  const apply = useMutation({
    // A delete carries the number the person typed, which the server
    // checks against the count itself; anything else carries nothing.
    mutationFn: () => applyBatch(cs!.id, cs!.digest, isDelete ? typed.trim() : ""),
    onSuccess: (r) => { setResult(r); onApplied(); },
  });
  const reverse = useMutation({
    mutationFn: () => reverseBatch(cs!.id),
    onSuccess: (data) => { setCs(data); setResult(null); setReversed(true); setTyped(""); },
  });

  const canApply =
    !!cs && cs.allowed && (!isDelete || cs.delete_allowed) && confirmed
    && count > 0 && !result && !apply.isPending && !expired;

  const footer = (
    <div
      className="flex flex-wrap items-center justify-between gap-2 px-5 py-3"
      style={{ borderTop: "1px solid var(--line)", background: "var(--surface)" }}
    >
      {cs && !cs.allowed ? (
        <WritesOffNote setting={WRITES} />
      ) : cs && isDelete && !cs.delete_allowed ? (
        <span className="text-[12.5px]" style={{ color: "var(--muted)" }}>
          Deleting is off for this deployment: <code className="font-mono text-[12px]">{DELETES}</code> is unset.
          The preview is all there is until it is set.
        </span>
      ) : (
        <span className="text-[12.5px]" style={{ color: expired ? "var(--warn)" : "var(--muted)" }}>
          {result
            ? "Applied. The backlog is being swept again."
            : !cs
              ? ""
              : expired
                ? "This preview has expired. Close and start again."
                : count === 0
                  ? "Nothing to write."
                  : `Writes ${count} ${count === 1 ? "ticket" : "tickets"}, one at a time, checking each is still as previewed.`}
        </span>
      )}
      <span className="flex items-center gap-2">
        {result && result.applied > 0 && !isDelete && (
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
            style={isDelete ? { background: "var(--crit)", color: "#fff" } : filled}
          >
            {apply.isPending ? "Writing…" : isDelete ? `Delete ${count}` : "Apply"}
          </button>
        )}
      </span>
    </div>
  );

  return (
    <SidePanel
      title={request.label}
      subtitle={`${request.keys.length} ${request.keys.length === 1 ? "ticket" : "tickets"} selected. Nothing is written until Apply.`}
      pending={false}
      onClose={onClose}
      footer={footer}
    >
      {preview.isPending && <p className="text-[13px]" style={{ color: "var(--muted)" }}>Asking the server what it would do…</p>}
      {preview.error && (
        <p className="rounded-lg px-3 py-2 text-[13px]" style={{ background: "var(--crit-bg)", color: "var(--crit)" }}>{errText(preview.error)}</p>
      )}

      {cs && (
        <>
          {reversed && (
            <p className="mb-3 rounded-lg px-3 py-2 text-[13px]" style={{ background: "var(--info-bg)", color: "var(--info)" }}>
              This is the reverse of the batch just applied. Applying it puts the fields back.
            </p>
          )}
          <Preview cs={cs} />

          {/* The one irreversible write, said in words and in a box that
              cannot be mistaken for the others. The count has to be typed
              because a button is pressed by habit and a number is not. */}
          {isDelete && !result && (
            <div className="mt-5 rounded-lg px-4 py-3" style={{ border: "2px solid var(--crit)", background: "var(--crit-bg)" }}>
              <p className="text-[13px] font-semibold" style={{ color: "var(--crit)" }}>
                This deletes {count} {count === 1 ? "ticket" : "tickets"} from Jira. It cannot be undone.
              </p>
              <p className="mt-1 text-[12.5px]" style={{ color: "var(--crit)" }}>
                There is no reverse for a delete. An audit row is kept per ticket, but the ticket itself is gone.
              </p>
              <label className="mt-2 flex flex-wrap items-center gap-2 text-[12.5px]" style={{ color: "var(--ink)" }}>
                Type {count} to enable the button
                <input
                  value={typed}
                  onChange={(e) => setTyped(e.target.value)}
                  inputMode="numeric"
                  disabled={!cs.delete_allowed || count === 0}
                  className="tnum w-20 rounded px-2 py-1 text-[13px] outline-none disabled:opacity-50"
                  style={{ background: "var(--surface)", border: "1px solid var(--line)", color: "var(--ink)" }}
                />
              </label>
            </div>
          )}

          {apply.error && (
            <p className="mt-2 rounded-lg px-3 py-2 text-[13px]" style={{ background: "var(--crit-bg)", color: "var(--crit)" }}>{errText(apply.error)}</p>
          )}
          {reverse.error && (
            <p className="mt-2 rounded-lg px-3 py-2 text-[13px]" style={{ background: "var(--crit-bg)", color: "var(--crit)" }}>{errText(reverse.error)}</p>
          )}
          {result && <Results r={result} />}
        </>
      )}
    </SidePanel>
  );
}
