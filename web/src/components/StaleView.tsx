import { PRTable, BranchTable, PR_FILTER_FIELDS } from "./Tables";
import { passes, useFilter } from "../tableQuery";
import { FilterNote } from "./Filters";
import type { BranchRow, PRRow } from "../api";

/* Stale pull requests and stale branches answer one question - what has
 * been abandoned? - so they belong in one place. They are different
 * shapes, which is why they render as two blocks rather than one table,
 * but making someone check two tabs for a single thought is worse than
 * a heading in the middle of a page. */
export function StaleView({
  prs,
  botCount,
  branches,
  controls = false,
}: {
  prs: PRRow[];
  botCount: number;
  branches: BranchRow[];
  /** Sort and filter the pull requests, as the tab does and the
   *  overview's preview does not. */
  controls?: boolean;
}) {
  // The branches answer to the same filter as the pull requests above
  // them, so one author's abandoned work is all of it.
  const filter = useFilter(PR_FILTER_FIELDS);
  const shownBranches = controls
    ? branches.filter((b) => passes(filter, { author: [b.author], repo: [b.repo] }, [b.repo, b.branch, b.author]))
    : branches;
  return (
    <div className="space-y-4">
      <section className="card overflow-hidden">
        <header className="px-4 pt-3.5 pb-2">
          <h3 className="text-[14px] font-semibold">Pull requests</h3>
          <p className="mt-0.5 text-[13px]" style={{ color: "var(--muted)" }}>
            Open longer than the age threshold.
            {botCount > 0 && ` ${botCount} bot pull requests are counted but not listed.`}
          </p>
        </header>
        <div style={{ borderTop: "1px solid var(--line)" }}>
          <PRTable rows={prs} controls={controls} />
        </div>
      </section>

      <section className="card overflow-hidden">
        <header className="px-4 pt-3.5 pb-2">
          <h3 className="text-[14px] font-semibold">Branches</h3>
          <p className="mt-0.5 text-[13px]" style={{ color: "var(--muted)" }}>
            No commit in a long time. Yours are flagged so you can delete them.
          </p>
        </header>
        <div style={{ borderTop: "1px solid var(--line)" }}>
          {branches.length > 0 && shownBranches.length === 0 ? null : <BranchTable rows={shownBranches} />}
          {controls && <FilterNote shown={shownBranches.length} total={branches.length} filter={filter} />}
        </div>
      </section>
    </div>
  );
}
