import type { JSX } from "react";
import { Badge } from "./Badge";
import { PRTable, BranchTable } from "./Tables";
import type { BranchRow, PRRow } from "../api";

/* The landing view: everything at once, in the order it should be dealt
 * with.
 *
 * Triage is the job, so the question "is anything waiting on me?" should
 * be answerable without clicking. Two things keep it from becoming a wall:
 * each block shows only its first few rows and links into its own tab for
 * the rest, and sections with nothing in them collapse into a single
 * reassuring line instead of taking a card each. */

export type Block = {
  id: string;
  title: string;
  why: string;
  count: number;
  /** Highest first. Things blocked on this person come before everything else. */
  priority: number;
  urgent?: boolean;
  render: (limit: number) => JSX.Element;
};

const PREVIEW_ROWS = 5;

export function Overview({
  blocks,
  onOpen,
}: {
  blocks: Block[];
  onOpen: (id: string) => void;
}) {
  const ordered = [...blocks].sort((a, b) => b.priority - a.priority);
  const active = ordered.filter((b) => b.count > 0);
  const clear = ordered.filter((b) => b.count === 0);

  return (
    <div className="space-y-4">
      {active.length === 0 && (
        <div className="card p-8 text-center">
          <p className="text-sm font-medium">Nothing needs you.</p>
          <p className="mt-1 text-sm" style={{ color: "var(--muted)" }}>
            No reviews waiting, nothing unclaimed, nothing stale.
          </p>
        </div>
      )}

      {active.map((b) => (
        <section key={b.id} className="card overflow-hidden">
          <header className="flex flex-wrap items-baseline justify-between gap-2 px-4 pt-3.5 pb-2">
            <div className="min-w-0">
              <div className="flex items-center gap-2">
                <h2 className="text-[15px] font-semibold">{b.title}</h2>
                {b.urgent && <Badge tone="critical" label={`${b.count}`} />}
              </div>
              <p className="mt-0.5 text-[13px]" style={{ color: "var(--muted)" }}>
                {b.why}
              </p>
            </div>
            {b.count > PREVIEW_ROWS && (
              <button onClick={() => onOpen(b.id)} className="lnk shrink-0 text-[13px] font-medium">
                View all {b.count} →
              </button>
            )}
          </header>
          <div style={{ borderTop: "1px solid var(--line)" }}>{b.render(PREVIEW_ROWS)}</div>
        </section>
      ))}

      {clear.length > 0 && (
        <div className="card flex flex-wrap items-center gap-x-2 gap-y-1 px-4 py-3 text-[13px]">
          <Badge tone="good" label="all clear" />
          <span style={{ color: "var(--muted)" }}>
            {clear.map((b, i) => (
              <span key={b.id}>
                {i > 0 && ", "}
                <button onClick={() => onOpen(b.id)} className="lnk">
                  {b.title.toLowerCase()}
                </button>
              </span>
            ))}
          </span>
        </div>
      )}
    </div>
  );
}

/** Row renderers, so Overview does not need to know each shape. */
export const preview = {
  prs: (rows: PRRow[], showAuthor = true) => (limit: number) => (
    <PRTable rows={rows.slice(0, limit)} showAuthor={showAuthor} />
  ),
  branches: (rows: BranchRow[]) => (limit: number) => <BranchTable rows={rows.slice(0, limit)} />,
};
