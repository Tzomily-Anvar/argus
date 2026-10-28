import { Badge, Pill, severityTone, type Tone } from "./Badge";
import type { BranchRow, PRRow } from "../api";
import { byText, passes, sortRows, useFilter, useSort, without, type Filter } from "../tableQuery";
import { FilterNote, SortTh } from "./Filters";

function Age({ days }: { days: number }) {
  return (
    <span className="tnum tabular-nums" style={{ color: days >= 30 ? "var(--warn)" : "var(--muted)" }}>
      {days}d
    </span>
  );
}

function Th({ children, className = "" }: { children?: React.ReactNode; className?: string }) {
  return (
    <th
      className={`px-4 py-2.5 text-left text-[11px] font-semibold tracking-wide uppercase ${className}`}
      style={{ color: "var(--faint)" }}
    >
      {children}
    </th>
  );
}

function Table({ head, children }: { head: React.ReactNode; children: React.ReactNode }) {
  return (
    <div className="overflow-x-auto">
      <table className="w-full border-collapse text-sm">
        <thead style={{ background: "var(--surface-2)" }}>
          <tr>{head}</tr>
        </thead>
        <tbody>{children}</tbody>
      </table>
    </div>
  );
}

function Row({ children }: { children: React.ReactNode }) {
  return (
    <tr className="[&:not(:last-child)]:border-b" style={{ borderColor: "var(--line)" }}>
      {children}
    </tr>
  );
}

function RepoLink({ row }: { row: PRRow }) {
  return (
    <a
      href={row.url}
      target="_blank"
      rel="noreferrer"
      className="font-mono text-xs hover:underline"
      style={{ color: "var(--accent)" }}
    >
      {row.repo}#{row.number}
    </a>
  );
}

/** Review decision as a status, with its own written label. */
function reviewBadge(row: PRRow) {
  const map: Record<string, { tone: Tone; label: string }> = {
    APPROVED: { tone: "good", label: "approved" },
    CHANGES_REQUESTED: { tone: "critical", label: "changes requested" },
    REVIEW_REQUIRED: { tone: "warning", label: "needs review" },
  };
  const hit = map[row.review_decision ?? ""];
  return hit ? <Badge tone={hit.tone} label={hit.label} /> : null;
}

/** How much a pull request needs someone, worst first: something
 *  failing, then changes asked for, then threads left open, then a
 *  policy gate, then a review still owed, and approved last. Sorting the
 *  status column by this says more than sorting by the badges' words. */
function badness(r: PRRow): number {
  if ((r.real_failures ?? []).length > 0) return 0;
  if (r.review_decision === "CHANGES_REQUESTED") return 1;
  if ((r.unresolved_threads ?? 0) > 0) return 2;
  if ((r.policy_failures ?? []).length > 0) return 3;
  if (r.review_decision === "REVIEW_REQUIRED") return 4;
  if (r.review_decision === "APPROVED") return 6;
  return 5;
}

const prOrder: Record<string, (a: PRRow, b: PRRow) => number> = {
  pr: (a, b) => byText(a.repo, b.repo) || a.number - b.number,
  title: (a, b) => byText(a.title, b.title),
  author: (a, b) => byText(a.author, b.author),
  status: (a, b) => badness(a) - badness(b),
  age: (a, b) => a.age_days - b.age_days,
};

/** The pull request filter's fields, as query parameters. */
export const PR_FILTER_FIELDS = ["author", "repo"];

export function prPasses(filter: Filter, r: PRRow): boolean {
  return passes(filter, { author: [r.author], repo: [r.repo] }, [`${r.repo}#${r.number}`, r.title, r.author]);
}

/** A table of pull requests. With `controls`, its headings sort it and
 *  the filter above it applies, both read from the address; without,
 *  it is a plain list, as the overview's previews are. */
export function PRTable({ rows, showAuthor = true, controls = false }: { rows: PRRow[]; showAuthor?: boolean; controls?: boolean }) {
  const [sort, onSort] = useSort();
  // Without an author column the table is one author's, and an author
  // picked on another tab is set aside here rather than emptying it.
  const all = useFilter(PR_FILTER_FIELDS);
  const filter = showAuthor ? all : without(all, ["author"]);
  if (rows.length === 0) return <Empty />;
  const shown = controls ? sortRows(rows.filter((r) => prPasses(filter, r)), sort, prOrder) : rows;
  // One row has no order to choose.
  const head = (label: string, col: string, className = "", startDesc = false) =>
    controls && rows.length > 1
      ? <SortTh label={label} col={col} sort={sort} onSort={onSort} startDesc={startDesc} className={`px-4 ${className}`} />
      : <Th className={className}>{label}</Th>;
  return (
    <>
      <Table
        head={
          <>
            {head("PR", "pr")}
            {head("Title", "title")}
            {showAuthor && head("Author", "author")}
            {head("Status", "status")}
            {head("Age", "age", "text-right", true)}
          </>
        }
      >
        {shown.map((row) => (
          <Row key={row.url}>
            <td className="px-4 py-2.5 align-top whitespace-nowrap">
              <RepoLink row={row} />
            </td>
            <td className="max-w-md px-4 py-2.5 align-top">
              <span className="line-clamp-2">{row.title}</span>
              {row.draft && (
                <span className="ml-2 align-middle">
                  <Pill title="Draft pull requests are included here on purpose">draft</Pill>
                </span>
              )}
            </td>
            {showAuthor && (
              <td className="px-4 py-2.5 align-top whitespace-nowrap" style={{ color: "var(--muted)" }}>
                {row.author}
              </td>
            )}
            <td className="px-4 py-2.5 align-top">
              <div className="flex flex-wrap items-center gap-1">
                {reviewBadge(row)}
                {(row.unresolved_threads ?? 0) > 0 && (
                  <Badge
                    tone="warning"
                    label={`${row.unresolved_threads} unresolved`}
                    title={`${row.unresolved_threads} review ${row.unresolved_threads === 1 ? "thread" : "threads"} not yet resolved`}
                  />
                )}
                {row.real_failures && row.real_failures.length > 0 && (
                  <Badge
                    tone="critical"
                    label={`${row.real_failures.length} failing`}
                    title={row.real_failures.join("\n")}
                  />
                )}
                {row.policy_failures && row.policy_failures.length > 0 && (
                  <Badge
                    tone="warning"
                    label="policy gate"
                    title={`Not a build failure - a policy check:\n${row.policy_failures.join("\n")}`}
                  />
                )}
                {row.qa_label_used && row.has_qa_label && <Badge tone="good" label="QA" />}
              </div>
            </td>
            <td className="px-4 py-2.5 text-right align-top">
              <Age days={row.age_days} />
            </td>
          </Row>
        ))}
      </Table>
      {controls && <FilterNote shown={shown.length} total={rows.length} filter={filter} />}
    </>
  );
}

export function BranchTable({ rows }: { rows: BranchRow[] }) {
  if (rows.length === 0) return <Empty />;
  return (
    <Table
      head={
        <>
          <Th>Repository</Th>
          <Th>Branch</Th>
          <Th>Author</Th>
          <Th className="text-right">Last commit</Th>
        </>
      }
    >
      {rows.map((row) => (
        <Row key={`${row.repo}/${row.branch}`}>
          <td className="px-4 py-2.5 font-mono text-xs whitespace-nowrap">{row.repo}</td>
          <td className="px-4 py-2.5">
            <a
              href={row.url}
              target="_blank"
              rel="noreferrer"
              className="font-mono text-xs hover:underline"
              style={{ color: "var(--accent)" }}
            >
              {row.branch}
            </a>
            {row.mine && (
              <span className="ml-2">
                <Badge tone="warning" label="yours" />
              </span>
            )}
          </td>
          <td className="px-4 py-2.5 whitespace-nowrap" style={{ color: "var(--muted)" }}>
            {row.author}
          </td>
          <td className="px-4 py-2.5 text-right whitespace-nowrap">
            <span className="tnum" style={{ color: "var(--muted)" }}>
              {row.last_commit}
            </span>{" "}
            <Age days={row.age_days} />
          </td>
        </Row>
      ))}
    </Table>
  );
}

export function SeverityRow({ counts }: { counts: Record<string, number> }) {
  const order = ["critical", "high", "medium", "low"];
  const present = order.filter((s) => counts[s]);
  if (present.length === 0) return null;
  return (
    <div className="flex flex-wrap gap-1.5">
      {present.map((sev) => (
        <Badge key={sev} tone={severityTone(sev)} label={`${counts[sev]} ${sev}`} />
      ))}
    </div>
  );
}

export function Empty() {
  return (
    <p className="px-4 py-8 text-center text-sm" style={{ color: "var(--faint)" }}>
      Nothing here — which is the good outcome.
    </p>
  );
}
