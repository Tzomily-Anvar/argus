import { Badge, severityTone } from "./Badge";
import { Empty, SeverityRow } from "./Tables";

type RepoCount = { repo: string; critical: number; high: number };
type Critical = { repo: string; package: string; scope: string; cve: string; manifest: string; url: string };

export type SecurityData = {
  dependabot?: {
    counts?: Record<string, number>;
    crit_high_by_repo?: RepoCount[];
    criticals?: Critical[];
    error?: string;
  };
  code_scanning?: { total?: number; crit_high?: { repo: string; severity: string; rule: string; url: string }[]; error?: string };
  dependabot_prs?: { total?: number; by_repo?: { repo: string; count: number }[]; error?: string };
};

function SubError({ message }: { message: string }) {
  return (
    <p className="px-3 py-3 text-sm" style={{ color: "var(--color-status-serious)" }}>
      {message}
    </p>
  );
}

function Sub({ title, note, children }: { title: string; note?: string; children: React.ReactNode }) {
  return (
    <section className="py-3">
      <h4 className="px-3 pb-2 text-xs font-medium tracking-wide uppercase" style={{ color: "var(--color-ink-faint)" }}>
        {title}
        {note && (
          <span className="ml-2 normal-case" style={{ color: "var(--color-ink-faint)" }}>
            {note}
          </span>
        )}
      </h4>
      {children}
    </section>
  );
}

export function Security({ data }: { data: SecurityData }) {
  const db = data.dependabot;
  const cs = data.code_scanning;
  const prs = data.dependabot_prs;

  return (
    <div className="divide-y" style={{ borderColor: "var(--color-line)" }}>
      <Sub title="Dependabot alerts" note="organisation-wide">
        {db?.error ? (
          <SubError message={db.error} />
        ) : (
          <div className="space-y-3 px-3">
            {db?.counts && <SeverityRow counts={db.counts} />}
            {db?.crit_high_by_repo && db.crit_high_by_repo.length > 0 ? (
              <ul className="space-y-1">
                {db.crit_high_by_repo.map((r) => (
                  <li key={r.repo} className="flex items-center justify-between gap-3 text-sm">
                    <span className="font-mono text-xs">{r.repo}</span>
                    <span className="flex gap-1.5">
                      {r.critical > 0 && <Badge tone="critical" label={`${r.critical} critical`} />}
                      {r.high > 0 && <Badge tone="serious" label={`${r.high} high`} />}
                    </span>
                  </li>
                ))}
              </ul>
            ) : (
              <p className="text-sm" style={{ color: "var(--color-ink-faint)" }}>
                No critical or high alerts outside vendored paths.
              </p>
            )}
          </div>
        )}
      </Sub>

      {db?.criticals && db.criticals.length > 0 && (
        <Sub title="Critical, in first-party code">
          <ul className="space-y-2 px-3">
            {db.criticals.map((c) => (
              <li key={c.url} className="text-sm">
                <a href={c.url} target="_blank" rel="noreferrer" className="hover:underline" style={{ color: "var(--color-accent)" }}>
                  <span className="font-mono text-xs">{c.repo}</span> — {c.package}
                </a>
                <span className="ml-2 text-xs" style={{ color: "var(--color-ink-muted)" }}>
                  {c.cve}
                  {c.scope ? ` · ${c.scope}` : ""}
                </span>
              </li>
            ))}
          </ul>
        </Sub>
      )}

      <Sub title="Code scanning">
        {cs?.error ? (
          <SubError message={cs.error} />
        ) : cs?.total ? (
          <div className="space-y-2 px-3">
            <p className="text-sm" style={{ color: "var(--color-ink-muted)" }}>
              <span className="tnum font-semibold" style={{ color: "var(--color-ink)" }}>
                {cs.total}
              </span>{" "}
              open, of which {cs.crit_high?.length ?? 0} critical or high.
            </p>
            <ul className="space-y-1">
              {(cs.crit_high ?? []).slice(0, 10).map((a) => (
                <li key={a.url} className="flex items-center justify-between gap-3 text-sm">
                  <a href={a.url} target="_blank" rel="noreferrer" className="truncate hover:underline" style={{ color: "var(--color-accent)" }}>
                    <span className="font-mono text-xs">{a.repo}</span> — {a.rule}
                  </a>
                  <Badge tone={severityTone(a.severity)} label={a.severity} />
                </li>
              ))}
            </ul>
          </div>
        ) : (
          <Empty />
        )}
      </Sub>

      <Sub title="Open Dependabot pull requests">
        {prs?.error ? (
          <SubError message={prs.error} />
        ) : (
          <div className="px-3">
            <p className="text-sm" style={{ color: "var(--color-ink-muted)" }}>
              <span className="tnum font-semibold" style={{ color: "var(--color-ink)" }}>
                {prs?.total ?? 0}
              </span>{" "}
              open across {prs?.by_repo?.length ?? 0} repositories.
            </p>
          </div>
        )}
      </Sub>
    </div>
  );
}
