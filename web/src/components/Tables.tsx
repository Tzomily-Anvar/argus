import { Badge, Pill, severityTone, type Tone } from "./Badge";
import type { BranchRow, PRRow } from "../api";

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

export function PRTable({ rows, showAuthor = true }: { rows: PRRow[]; showAuthor?: boolean }) {
  if (rows.length === 0) return <Empty />;
  return (
    <Table
      head={
        <>
          <Th>PR</Th>
          <Th>Title</Th>
          {showAuthor && <Th>Author</Th>}
          <Th>Status</Th>
          <Th className="text-right">Age</Th>
        </>
      }
    >
      {rows.map((row) => (
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
