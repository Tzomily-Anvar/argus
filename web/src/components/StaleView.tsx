import { PRTable, BranchTable } from "./Tables";
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
}: {
  prs: PRRow[];
  botCount: number;
  branches: BranchRow[];
}) {
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
          <PRTable rows={prs} />
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
          <BranchTable rows={branches} />
        </div>
      </section>
    </div>
  );
}
