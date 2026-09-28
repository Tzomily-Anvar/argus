import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { acknowledgeBacklog, unacknowledgeBacklog, type BacklogRow } from "../../api";
import { outlined, small } from "../closeout/Table";
import { Rows, type Selection } from "./Rows";
import { Section } from "./Section";

/* New: what has arrived or changed since it was last looked at.
 *
 * Acknowledging is a watermark kept by Argus on the ticket's `updated`,
 * and nothing is written to Jira. A ticket acknowledged today hides from
 * this list until it changes again, when it comes back as new; so the
 * list is a reading of "what have I not seen", not of "what is recent".
 * The acknowledged rows can be shown, faded, for the case where one was
 * acknowledged by mistake. */

const byNewest = (a: BacklogRow, b: BacklogRow) => (b.created > a.created ? 1 : b.created < a.created ? -1 : 0);

export function NewView({
  rows, selection, staleKeys,
}: {
  rows: BacklogRow[];
  selection: Selection;
  staleKeys: Set<string>;
}) {
  const qc = useQueryClient();
  const [showAcked, setShowAcked] = useState(false);
  const done = () => qc.invalidateQueries({ queryKey: ["backlog"] });
  const ack = useMutation({ mutationFn: acknowledgeBacklog, onSuccess: done });
  const unack = useMutation({ mutationFn: unacknowledgeBacklog, onSuccess: done });

  const fresh = (rows ?? []).filter((r) => r.new && !r.acknowledged).sort(byNewest);
  const acked = (rows ?? []).filter((r) => r.acknowledged).sort(byNewest);
  const shown = showAcked ? [...fresh, ...acked] : fresh;
  const busy = ack.isPending || unack.isPending;
  const err = ack.error ?? unack.error;

  return (
    <Section
      title={`New · ${fresh.length}`}
      hint="Created or changed since you last acknowledged them, newest first. Acknowledging is kept here, not in Jira: the ticket hides until it changes again."
      right={
        <>
          <label className="flex items-center gap-1.5 text-[12.5px]" style={{ color: "var(--muted)" }}>
            <input type="checkbox" checked={showAcked} onChange={(e) => setShowAcked(e.target.checked)} />
            Show acknowledged ({acked.length})
          </label>
          <button
            onClick={() => ack.mutate(fresh.map((r) => r.key))}
            disabled={busy || fresh.length === 0}
            className={small}
            style={outlined}
          >
            Acknowledge all shown
          </button>
        </>
      }
    >
      {err && (
        <p className="px-4 py-2 text-[13px]" style={{ background: "var(--crit-bg)", color: "var(--crit)" }}>
          {err instanceof Error ? err.message : String(err)}
        </p>
      )}
      <Rows
        rows={shown}
        selection={selection}
        staleKeys={staleKeys}
        dim={(r) => r.acknowledged}
        empty="Nothing new. Everything open has been seen since it last changed."
        action={(r) =>
          r.acknowledged ? (
            <button onClick={() => unack.mutate([r.key])} disabled={busy} className={small} style={outlined}>
              Unacknowledge
            </button>
          ) : (
            <button onClick={() => ack.mutate([r.key])} disabled={busy} className={small} style={outlined}>
              Acknowledge
            </button>
          )
        }
      />
    </Section>
  );
}
