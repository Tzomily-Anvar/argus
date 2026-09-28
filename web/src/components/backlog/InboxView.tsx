import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { dismissInbox, undismissInbox, type Inbox, type InboxItem } from "../../api";
import { Badge } from "../Badge";
import { outlined, small } from "../closeout/Table";
import { Section, ago } from "./Section";

/* Inbox: the personal feed, across projects.
 *
 * Mentions, assignments and watched pages from Jira and Confluence,
 * grouped by where they came from and why they are here. Dismissing is
 * the same watermark the New view uses: kept by Argus, nothing written
 * anywhere. Opening an item counts as dismissing it, as it did in the
 * old tool, because a thing opened has been seen. */

const SOURCES: InboxItem["source"][] = ["jira", "confluence"];
const KINDS: InboxItem["kind"][] = ["mentioned", "assigned", "watching"];
const SOURCE_LABEL: Record<InboxItem["source"], string> = { jira: "Jira", confluence: "Confluence" };

export function InboxView({ inbox, loading, error }: { inbox?: Inbox; loading: boolean; error: unknown }) {
  const qc = useQueryClient();
  const [showDismissed, setShowDismissed] = useState(false);
  const done = () => qc.invalidateQueries({ queryKey: ["inbox"] });
  const dismiss = useMutation({ mutationFn: dismissInbox, onSuccess: done });
  const undismiss = useMutation({ mutationFn: undismissInbox, onSuccess: done });

  const items = inbox?.items ?? [];
  const live = items.filter((i) => !i.dismissed);
  const shown = showDismissed ? items : live;
  const busy = dismiss.isPending || undismiss.isPending;
  const err = error ?? dismiss.error ?? undismiss.error;

  // Every source and kind, in a fixed order, skipping the empty ones: a
  // heading with nothing under it is a question the reader cannot tell
  // from an answer.
  const groups = SOURCES.flatMap((source) =>
    KINDS.map((kind) => ({ source, kind, items: shown.filter((i) => i.source === source && i.kind === kind) })),
  ).filter((g) => g.items.length > 0);

  return (
    <Section
      title={`Inbox · ${live.length}`}
      hint="Mentions, assignments and watched pages, across Jira and Confluence. Opening one dismisses it; dismissing is kept here, and nothing is written back."
      right={
        <label className="flex items-center gap-1.5 text-[12.5px]" style={{ color: "var(--muted)" }}>
          <input type="checkbox" checked={showDismissed} onChange={(e) => setShowDismissed(e.target.checked)} />
          Show dismissed ({items.length - live.length})
        </label>
      }
    >
      {(inbox?.warnings ?? []).length > 0 && (
        <p className="px-4 py-2 text-[13px]" style={{ background: "var(--warn-bg)", color: "var(--warn)" }}>
          {(inbox?.warnings ?? []).join(" · ")}
        </p>
      )}
      {err ? (
        <p className="px-4 py-2 text-[13px]" style={{ background: "var(--crit-bg)", color: "var(--crit)" }}>
          {err instanceof Error ? err.message : String(err)}
        </p>
      ) : null}
      {loading && <p className="px-4 py-6 text-center text-sm" style={{ color: "var(--muted)" }}>Reading the feed…</p>}
      {!loading && groups.length === 0 && (
        <p className="px-4 py-6 text-center text-sm" style={{ color: "var(--faint)" }}>Nothing waiting for you.</p>
      )}
      {groups.map((g) => (
        <div key={`${g.source}-${g.kind}`}>
          <h3
            className="px-4 py-2 text-[11px] font-semibold tracking-wide uppercase"
            style={{ background: "var(--surface-2)", color: "var(--faint)" }}
          >
            {SOURCE_LABEL[g.source]} · {g.kind} · {g.items.length}
          </h3>
          <ul className="divide-y" style={{ borderColor: "var(--line)" }}>
            {g.items.map((i) => (
              <li
                key={i.id}
                className="flex flex-wrap items-center gap-2 px-4 py-2.5 text-[13px]"
                style={{ opacity: i.dismissed ? 0.55 : 1 }}
              >
                <div className="min-w-0 flex-1">
                  <a
                    href={i.url || undefined}
                    target="_blank"
                    rel="noreferrer"
                    className="lnk"
                    onClick={() => { if (!i.dismissed) dismiss.mutate([i.id]); }}
                  >
                    {i.title}
                  </a>
                  {i.summary && (
                    <div className="truncate text-[12px]" style={{ color: "var(--muted)" }} title={i.summary}>{i.summary}</div>
                  )}
                </div>
                {i.dismissed && <Badge tone="neutral" label="dismissed" />}
                <span className="shrink-0 text-xs" style={{ color: "var(--faint)" }}>{ago(i.updated)}</span>
                {i.dismissed ? (
                  <button onClick={() => undismiss.mutate([i.id])} disabled={busy} className={small} style={outlined}>Undismiss</button>
                ) : (
                  <button onClick={() => dismiss.mutate([i.id])} disabled={busy} className={small} style={outlined}>Dismiss</button>
                )}
              </li>
            ))}
          </ul>
        </div>
      ))}
    </Section>
  );
}
