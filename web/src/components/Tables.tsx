import { Badge, Pill, severityTone, type Tone } from "./Badge";
import type { BranchRow, PRRow } from "../api";
import { byText, passes, sortRows, useFilter, useSort, without, type Filter } from "../tableQuery";
import { FilterNote, SortTh } from "./Filters";
import { useLooked } from "../looked";

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
    <tr className="group [&:not(:last-child)]:border-b" style={{ borderColor: "var(--line)" }}>
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

/** Review decision as a status, with its own written label, and the
 *  approvals against what the rulesets ask for where they ask. A pull
 *  request GitHub calls approved on fewer than that - one stacked on a
 *  branch the rulesets do not cover - says so rather than wearing a tick. */
function reviewBadge(row: PRRow) {
  const map: Record<string, { tone: Tone; label: string }> = {
    APPROVED: { tone: "good", label: "approved" },
    CHANGES_REQUESTED: { tone: "critical", label: "changes requested" },
    REVIEW_REQUIRED: { tone: "warning", label: "needs review" },
  };
  const hit = map[row.review_decision ?? ""];
  if (!hit) return null;
  const need = row.approvals_required ?? 0;
  const count = `${row.approvals ?? 0}/${need}`;
  if (need > 0 && row.github_decision === "APPROVED" && row.review_decision !== "APPROVED") {
    return (
      <Badge
        tone="warning"
        label={`${count} approvals`}
        title={`GitHub counts this approved, because ${row.base_branch ? `${row.base_branch} asks` : "the branch it merges into asks"} for fewer. The default branch asks for ${need}.`}
      />
    );
  }
  const counted = need > 0 && row.review_decision !== "CHANGES_REQUESTED";
  return <Badge tone={hit.tone} label={counted ? `${hit.label} ${count}` : hit.label} />;
}

/** The branch a pull request merges into, shown only where it is not
 *  the default branch: a stacked pull request lands on its parent, not
 *  in the main line. */
function BaseBranch({ name }: { name?: string }) {
  if (!name) return null;
  const short = name.length > 28 ? `${name.slice(0, 27)}…` : name;
  return (
    <span className="ml-2 align-middle">
      <Pill title={`Merges into ${name}, not the default branch`}>into {short}</Pill>
    </span>
  );
}

const SAID: Record<string, string> = {
  APPROVED: "you approved",
  CHANGES_REQUESTED: "you asked for changes",
  COMMENTED: "you commented",
};

/** Where a pull request stands with you: your review on the code as it
 *  stands, a seen mark, or a warning that it has moved since either.
 *  Marking it seen is offered where there is no review on the current
 *  code, and shows on hover so a long table is not a column of buttons. */
function LookedMark({ row }: { row: PRRow }) {
  const looked = useLooked();
  if (!looked) return null;
  const state = looked.of(row);
  if (state === "yours") return null;
  const said = SAID[looked.review(row)];
  const canMark = looked.available && looked.head(row) !== "";
  const markButton = canMark && (
    <button
      onClick={() => looked.mark(row, true)}
      disabled={looked.busy}
      className="lnk text-[12px] opacity-0 group-hover:opacity-100 focus-visible:opacity-100 [@media(hover:none)]:opacity-100"
      title="Mark it seen here, without a review in GitHub. It comes back as changed when someone pushes."
    >
      mark seen
    </button>
  );
  return (
    <span className="ml-2 inline-flex items-center gap-1.5 align-middle">
      {state === "reviewed" && said && <Pill title="Your latest review is on the code as it stands">{said}</Pill>}
      {state === "seen" && (
        <Pill title="You marked this seen, and nothing has been pushed since">
          seen
          <button
            onClick={() => looked.mark(row, false)}
            disabled={looked.busy}
            aria-label="Unmark seen"
            title="Unmark seen"
            className="ml-1"
            style={{ color: "var(--faint)" }}
          >
            ×
          </button>
        </Pill>
      )}
      {state === "changed since" && (
        <Badge
          tone="warning"
          label={said ? "changed since your review" : "changed since you looked"}
          title={said ? `Commits pushed since ${said}` : "Commits pushed since you marked it seen"}
        />
      )}
      {state !== "reviewed" && state !== "seen" && markButton}
    </span>
  );
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

/** The pull request filter's fields, as query parameters. `looked` is
 *  where a pull request stands with you: see looked.tsx. */
export const PR_FILTER_FIELDS = ["author", "repo", "looked"];

export function prPasses(filter: Filter, r: PRRow, looked = ""): boolean {
  return passes(
    filter,
    { author: [r.author], repo: [r.repo], looked: looked ? [looked] : [] },
    [`${r.repo}#${r.number}`, r.title, r.author],
  );
}

/** A table of pull requests. With `controls`, its headings sort it and
 *  the filter above it applies, both read from the address; without,
 *  it is a plain list, as the overview's previews are. */
export function PRTable({ rows, showAuthor = true, controls = false }: { rows: PRRow[]; showAuthor?: boolean; controls?: boolean }) {
  const [sort, onSort] = useSort();
  // Without an author column the table is one author's, and an author
  // picked on another tab is set aside here rather than emptying it.
  // Your own pull requests have nothing for you to look at, and with no
  // marks to read the looked filter could only empty the table.
  const all = useFilter(PR_FILTER_FIELDS);
  const looked = useLooked();
  const set = [...(showAuthor ? [] : ["author", "looked"]), ...(looked ? [] : ["looked"])];
  const filter = set.length > 0 ? without(all, set) : all;
  if (rows.length === 0) return <Empty />;
  const shown = controls
    ? sortRows(rows.filter((r) => prPasses(filter, r, looked?.of(r))), sort, prOrder)
    : rows;
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
              <BaseBranch name={row.base_branch} />
              <LookedMark row={row} />
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
