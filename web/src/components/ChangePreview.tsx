import { useEffect, useRef } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { fetchWritesStatus, proposeChanges, type Change, type ChangeRequest, type ChangeSet } from "../api";
import { figure, opLabel } from "../changes";
import { Badge } from "./Badge";
import { SidePanel } from "./Panel";

/* The preview: what the server would write, and what it would not.
 *
 * The draft is posted once when this opens and the answer is shown as a
 * plain table, one row per change, with the skipped requests and their
 * reasons underneath. Nothing here applies anything, and there is no
 * endpoint that could: applying arrives in a later release. With writes
 * off the same table renders and the button is replaced by the line
 * naming the setting, so the proposals can be judged before anybody
 * switches writing on. */

/** What the field holds now, as the guard recorded it. A field edit is
 *  only ever proposed against an empty field; a worklog correction is
 *  against the entry's current hours. */
function fromOf(c: Change): string {
  if (c.op === "worklog.add") return "—";
  if (c.guard.entry) return `${figure(c.guard.entry.seconds / 3600)}h`;
  return c.guard.was == null ? "(empty)" : String(c.guard.was);
}

/** The expiry as a time on the viewer's own clock, with how long that
 *  is from now. The viewer's clock is the right one here: the question
 *  is "how long have I got", not which day a sprint started on. */
function expiry(iso: string): string {
  const at = new Date(iso);
  const mins = Math.max(0, Math.round((at.getTime() - Date.now()) / 60_000));
  const clock = at.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
  return `${clock} (in ${mins} min)`;
}

function Table({ cs }: { cs: ChangeSet }) {
  if (cs.changes.length === 0) {
    return (
      <p className="rounded-lg px-4 py-6 text-center text-[13px]" style={{ background: "var(--surface-2)", color: "var(--muted)" }}>
        Nothing would be written. Every request was skipped; the reasons are below.
      </p>
    );
  }
  const th = "pb-2 text-left text-[11px] font-semibold tracking-wide uppercase";
  return (
    <table className="w-full border-collapse text-[13px]">
      <thead>
        <tr>
          {["Key", "What", "From", "To", "Reason"].map((h) => (
            <th key={h} className={th} style={{ color: "var(--faint)" }}>{h}</th>
          ))}
        </tr>
      </thead>
      <tbody>
        {cs.changes.map((c, i) => (
          <tr key={i} className="border-t align-top" style={{ borderColor: "var(--line)" }}>
            <td className="py-2 pr-3">
              <a href={c.url || undefined} target="_blank" rel="noreferrer" className="lnk font-mono text-xs">{c.key}</a>
              <div className="max-w-40 truncate text-[11.5px]" style={{ color: "var(--faint)" }} title={c.summary}>{c.summary}</div>
            </td>
            <td className="py-2 pr-3 whitespace-nowrap">{opLabel(c.op)}</td>
            <td className="tnum py-2 pr-3 whitespace-nowrap" style={{ color: "var(--muted)" }}>{fromOf(c)}</td>
            <td className="py-2 pr-3 font-medium">
              <span style={{ color: "var(--faint)" }}>→ </span>{c.after_label}
            </td>
            <td className="py-2" style={{ color: "var(--muted)" }}>{c.reason}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

export function ChangePreview({
  sprintNumber, requests, onClose,
}: {
  sprintNumber: number;
  requests: ChangeRequest[];
  onClose: () => void;
}) {
  const preview = useMutation({ mutationFn: () => proposeChanges(sprintNumber, requests) });
  // Once, on open. The draft is what it was when Preview was pressed;
  // changing it means pressing Preview again, which opens a fresh one.
  // The ref keeps a development-mode double effect from holding two
  // change sets on the server for one press.
  const asked = useRef(false);
  useEffect(() => {
    if (asked.current) return;
    asked.current = true;
    preview.mutate();
  }, [preview]);

  const writes = useQuery({ queryKey: ["writes-status"], queryFn: fetchWritesStatus, staleTime: 60_000 });
  const cs = preview.data;
  const count = cs?.changes.length ?? 0;

  const footer = (
    <div
      className="flex flex-wrap items-center justify-between gap-2 px-5 py-3"
      style={{ borderTop: "1px solid var(--line)", background: "var(--surface)" }}
    >
      {writes.data && !writes.data.allowed ? (
        <span className="text-[12.5px]" style={{ color: "var(--muted)" }}>
          Writes to Jira are off for this deployment: <code className="font-mono text-[12px]">{writes.data.setting}</code> is unset.
          The preview is all there is until it is set.
        </span>
      ) : (
        <>
          <span className="text-[12.5px]" style={{ color: "var(--muted)" }}>
            Applying arrives in a later release; nothing is written yet.
          </span>
          <button
            disabled
            title="Applying arrives in a later release; nothing is written yet."
            className="rounded-lg px-3 py-1.5 text-sm font-medium disabled:opacity-40"
            style={{ background: "var(--accent)", color: "#fff" }}
          >
            Write {count} {count === 1 ? "change" : "changes"} to Jira
          </button>
        </>
      )}
    </div>
  );

  return (
    <SidePanel
      title={`Preview: sprint ${sprintNumber}`}
      subtitle="What would be written to Jira, and what would not. Nothing is written from this screen."
      pending={false}
      onClose={onClose}
      footer={footer}
    >
      {preview.isPending && (
        <p className="text-[13px]" style={{ color: "var(--muted)" }}>Asking the server what it would do…</p>
      )}
      {preview.error && (
        <p className="rounded-lg px-3 py-2 text-[13px]" style={{ background: "var(--crit-bg)", color: "var(--crit)" }}>
          {preview.error instanceof Error ? preview.error.message : String(preview.error)}
        </p>
      )}
      {cs && (
        <>
          <div className="mb-3 flex flex-wrap items-center gap-2 text-[12.5px]" style={{ color: "var(--muted)" }}>
            <Badge tone={count > 0 ? "info" : "neutral"} label={`${count} ${count === 1 ? "change" : "changes"}`} />
            {cs.skipped.length > 0 && <Badge tone="warning" label={`${cs.skipped.length} skipped`} />}
            <span className="ml-auto">Valid until {expiry(cs.expires_at)}</span>
          </div>

          <Table cs={cs} />

          {cs.skipped.length > 0 && (
            <div className="mt-5">
              <h3 className="mb-1 text-[11px] font-semibold tracking-wide uppercase" style={{ color: "var(--faint)" }}>
                Skipped, and why
              </h3>
              <ul className="divide-y text-[13px]" style={{ borderColor: "var(--line)" }}>
                {cs.skipped.map((s, i) => (
                  <li key={i} className="flex flex-wrap items-baseline gap-2 py-1.5">
                    <span className="font-mono text-xs">{s.key}</span>
                    <span style={{ color: "var(--muted)" }}>{opLabel(s.op)}</span>
                    <span className="flex-1">{s.reason}</span>
                  </li>
                ))}
              </ul>
            </div>
          )}

          {/* Author is not attribution. Every edit carries the configured
              account as its author; the change names who did the work. */}
          <p className="mt-5 text-[12.5px]" style={{ color: "var(--muted)" }}>
            Edits will appear in Jira as <strong style={{ color: "var(--ink)" }}>{cs.actor || "the configured Jira account"}</strong>.
            Where a change names a person, the entry credits them by mention; the author is the account that recorded it.
          </p>
        </>
      )}
    </SidePanel>
  );
}
