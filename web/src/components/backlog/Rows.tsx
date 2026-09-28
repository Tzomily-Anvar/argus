import type { ReactNode } from "react";
import type { BacklogRow } from "../../api";
import { Badge, Pill } from "../Badge";

/* The table of tickets, one copy for every view.
 *
 * The four views ask different questions of the same rows - which are
 * new, which sit in a group, which are Operations - but the answer is
 * always a list of tickets, and a list of tickets should read the same
 * wherever it appears. One table, so a person who has learned the New
 * view has learned the Groups view, and so the checkbox in each means
 * the same thing: the selection is one set for the whole tool, and a
 * row ticked here stays ticked when the tab changes. */

/** The selection, lifted to the tool and shared across the views. A
 *  set of keys and the three ways of changing it. */
export type Selection = {
  keys: string[];
  has: (key: string) => boolean;
  toggle: (key: string) => void;
  set: (keys: string[], on: boolean) => void;
  clear: () => void;
};

/** Where Jira lives, read off a ticket's own URL rather than configured
 *  twice. Empty when no row has one, in which case nothing can be opened
 *  and the links are left out rather than pointed at nothing. */
export function jiraBase(rows: { url: string }[]): string {
  const url = rows.find((r) => r.url)?.url ?? "";
  const i = url.indexOf("/browse/");
  if (i > 0) return url.slice(0, i);
  try {
    return url ? new URL(url).origin : "";
  } catch {
    return "";
  }
}

/** The issue navigator, filtered by a JQL string. */
export function jqlURL(base: string, jql: string): string {
  return base ? `${base}/issues/?jql=${encodeURIComponent(jql)}` : "";
}

/** The JQL that names a set of keys, for "open in Jira" over a selection. */
export function keysJQL(keys: string[]): string {
  return `key in (${keys.join(", ")})`;
}

/** Whole days since a timestamp, for the age column. */
export function daysSince(iso: string): number {
  const ms = Date.now() - new Date(iso).getTime();
  return Number.isNaN(ms) ? 0 : Math.max(0, Math.floor(ms / 86_400_000));
}

function Th({ children, className = "" }: { children?: ReactNode; className?: string }) {
  return (
    <th
      className={`px-3 py-2.5 text-left text-[11px] font-semibold tracking-wide uppercase ${className}`}
      style={{ color: "var(--faint)" }}
    >
      {children}
    </th>
  );
}

/** A short chip for the epic, story or sprint a ticket sits in, or a
 *  dash when it sits in none. The dash is a value: "no epic" is one of
 *  the things this tool exists to show. */
function Chip({ label, title }: { label?: string; title?: string }) {
  if (!label) return <span style={{ color: "var(--faint)" }}>—</span>;
  return <Pill title={title}>{label}</Pill>;
}

/** The written flags on a row. Each pairs its colour with a label and a
 *  glyph, so nothing here is a colour alone. */
function Flags({ row, stale }: { row: BacklogRow; stale: boolean }) {
  return (
    <span className="flex flex-wrap gap-1">
      {row.new && !row.acknowledged && <Badge tone="info" label="new" title="Created or changed since you last acknowledged it" />}
      {row.acknowledged && <Badge tone="neutral" label="acknowledged" title="Hidden from New until it changes again" />}
      {stale && <Badge tone="warning" label={`stale ${row.stale_days}d`} title="Nothing has happened to it for longer than the team's threshold" />}
      {row.operations && <Badge tone="info" label="operations" title="An Operations request: by label, legacy label or roster reporter" />}
      {row.operations_missing_label && <Badge tone="warning" label="missing label" title="Reported by the Operations roster but not labelled as such" />}
      {row.legacy_label && <Badge tone="warning" label="legacy label" title="Carries the old label rather than the current one" />}
    </span>
  );
}

export function Rows({
  rows, selection, staleKeys, dim, action, empty = "Nothing here.",
}: {
  rows: BacklogRow[];
  selection: Selection;
  /** The keys the sweep put in the stale group, so the badge says how
   *  long only where it crosses the threshold. */
  staleKeys?: Set<string>;
  /** Rows to draw faded: acknowledged ones shown on request. */
  dim?: (row: BacklogRow) => boolean;
  /** A per-row control in the last column, such as Acknowledge. */
  action?: (row: BacklogRow) => ReactNode;
  empty?: string;
}) {
  const list = rows ?? [];
  if (list.length === 0) {
    return (
      <p className="px-4 py-6 text-center text-sm" style={{ color: "var(--faint)" }}>{empty}</p>
    );
  }
  const allOn = list.every((r) => selection.has(r.key));
  const someOn = !allOn && list.some((r) => selection.has(r.key));

  return (
    <div className="overflow-x-auto">
      <table className="w-full border-collapse text-[13px]">
        <thead style={{ background: "var(--surface-2)" }}>
          <tr>
            <Th className="w-8">
              <input
                type="checkbox"
                aria-label="Select all shown"
                checked={allOn}
                ref={(el) => { if (el) el.indeterminate = someOn; }}
                onChange={() => selection.set(list.map((r) => r.key), !allOn)}
              />
            </Th>
            <Th>Ticket</Th>
            <Th>Type</Th>
            <Th>Status</Th>
            <Th>Reporter</Th>
            <Th>Labels</Th>
            <Th>Epic · Story · Sprint</Th>
            <Th className="text-right">Age</Th>
            <Th>Flags</Th>
            {action && <Th />}
          </tr>
        </thead>
        <tbody>
          {list.map((r) => {
            const faded = dim ? dim(r) : false;
            return (
              <tr
                key={r.key}
                className="align-top [&:not(:last-child)]:border-b"
                style={{ borderColor: "var(--line)", opacity: faded ? 0.55 : 1 }}
              >
                <td className="px-3 py-2.5">
                  <input
                    type="checkbox"
                    aria-label={`Select ${r.key}`}
                    checked={selection.has(r.key)}
                    onChange={() => selection.toggle(r.key)}
                  />
                </td>
                <td className="px-3 py-2.5">
                  <a href={r.url || undefined} target="_blank" rel="noreferrer" className="lnk font-mono text-xs">{r.key}</a>
                  <div className="max-w-72 truncate" title={r.summary}>{r.summary}</div>
                </td>
                <td className="px-3 py-2.5 whitespace-nowrap" style={{ color: "var(--muted)" }}>{r.type}</td>
                <td className="px-3 py-2.5 whitespace-nowrap" style={{ color: "var(--muted)" }}>{r.status}</td>
                <td className="px-3 py-2.5 whitespace-nowrap" style={{ color: "var(--muted)" }}>{r.reporter?.label || "—"}</td>
                <td className="px-3 py-2.5">
                  {(r.labels ?? []).length === 0 ? (
                    <span style={{ color: "var(--faint)" }}>—</span>
                  ) : (
                    <span className="flex flex-wrap gap-1">
                      {(r.labels ?? []).map((l) => <Pill key={l}>{l}</Pill>)}
                    </span>
                  )}
                </td>
                <td className="px-3 py-2.5">
                  <span className="flex flex-wrap items-center gap-1">
                    <Chip label={r.epic?.key} title={r.epic ? `Epic: ${r.epic.summary}` : undefined} />
                    <Chip label={r.story?.key} title={r.story ? `Story: ${r.story.summary}` : undefined} />
                    <Chip label={r.sprint?.name} title={r.sprint ? `Sprint, ${r.sprint.state}` : undefined} />
                  </span>
                </td>
                <td className="tnum px-3 py-2.5 text-right whitespace-nowrap" style={{ color: "var(--muted)" }}>
                  {daysSince(r.created)}d
                </td>
                <td className="px-3 py-2.5"><Flags row={r} stale={staleKeys?.has(r.key) ?? false} /></td>
                {action && <td className="px-3 py-2.5 text-right whitespace-nowrap">{action(r)}</td>}
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
