import { useState } from "react";
import type { BacklogEpic, BacklogGroup, BacklogRow } from "../../api";
import { Badge } from "../Badge";
import { outlined, small } from "../closeout/Table";
import { jqlURL, Rows, type Selection } from "./Rows";
import { Section } from "./Section";

/* Groups: the fixed questions asked of the backlog, one card each.
 *
 * Each card carries its count, the sentence saying why the tickets are
 * in it, the JQL that reproduces the list in Jira, and the rows behind
 * a toggle. The JQL matters more than it looks: a count a person cannot
 * check in Jira is a number to argue with, and one they can is a number
 * to act on. Under the groups, the epics with work still to refine. */

function GroupCard({
  group, byKey, base, selection, staleKeys,
}: {
  group: BacklogGroup;
  byKey: Map<string, BacklogRow>;
  base: string;
  selection: Selection;
  staleKeys: Set<string>;
}) {
  const [open, setOpen] = useState(false);
  const keys = group.keys ?? [];
  const rows = keys.map((k) => byKey.get(k)).filter((r): r is BacklogRow => !!r);
  const link = group.jql_url || (group.jql ? jqlURL(base, group.jql) : "");

  return (
    <Section
      title={
        <span className="flex items-center gap-2">
          {group.label}
          <Badge tone={keys.length > 0 ? "info" : "neutral"} label={String(keys.length)} title={`${keys.length} in this group`} />
        </span>
      }
      hint={group.why}
      right={
        <>
          {link && (
            <a href={link} target="_blank" rel="noreferrer" className="lnk text-xs" title={group.jql}>
              Open in Jira →
            </a>
          )}
          <button onClick={() => selection.set(keys, true)} disabled={keys.length === 0} className={small} style={outlined}>
            Select all
          </button>
          <button onClick={() => setOpen((v) => !v)} aria-expanded={open} disabled={keys.length === 0} className={small} style={outlined}>
            {open ? "Hide" : "Show"}
          </button>
        </>
      }
    >
      {open ? (
        <Rows rows={rows} selection={selection} staleKeys={staleKeys} empty="None of these are in the sweep." />
      ) : (
        <p className="px-4 py-2 text-[12.5px]" style={{ color: "var(--faint)" }}>
          {keys.length === 0 ? "Nothing in this group." : `${keys.length} ${keys.length === 1 ? "ticket" : "tickets"}. Show to list them.`}
        </p>
      )}
    </Section>
  );
}

function Epics({ epics, base, selection }: { epics: BacklogEpic[]; base: string; selection: Selection }) {
  const th = "px-4 py-2.5 text-left text-[11px] font-semibold tracking-wide uppercase";
  if (epics.length === 0) {
    return <p className="px-4 py-6 text-center text-sm" style={{ color: "var(--faint)" }}>No epic has open work under it.</p>;
  }
  return (
    <div className="overflow-x-auto">
      <table className="w-full border-collapse text-[13px]">
        <thead style={{ background: "var(--surface-2)" }}>
          <tr>
            <th className={th} style={{ color: "var(--faint)" }}>Epic</th>
            <th className={th} style={{ color: "var(--faint)" }}>Class</th>
            <th className={`${th} text-right`} style={{ color: "var(--faint)" }}>Open</th>
            <th className={`${th} text-right`} style={{ color: "var(--faint)" }}>Unrefined</th>
            <th className={th} />
          </tr>
        </thead>
        <tbody>
          {epics.map((e) => (
            <tr key={e.key} className="[&:not(:last-child)]:border-b" style={{ borderColor: "var(--line)" }}>
              <td className="px-4 py-2.5">
                {base ? (
                  <a href={`${base}/browse/${e.key}`} target="_blank" rel="noreferrer" className="lnk font-mono text-xs">{e.key}</a>
                ) : (
                  <span className="font-mono text-xs">{e.key}</span>
                )}
                <span className="ml-2">{e.summary}</span>
              </td>
              <td className="px-4 py-2.5" style={{ color: "var(--muted)" }}>{e.class || "—"}</td>
              <td className="tnum px-4 py-2.5 text-right">{e.open}</td>
              <td className="tnum px-4 py-2.5 text-right" style={{ color: e.unrefined > 0 ? "var(--warn)" : "var(--muted)" }}>
                {e.unrefined}
              </td>
              <td className="px-4 py-2.5 text-right">
                <button
                  onClick={() => selection.set(e.keys ?? [], true)}
                  disabled={(e.keys ?? []).length === 0}
                  className={small}
                  style={outlined}
                >
                  Select unrefined
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

export function GroupsView({
  groups, epics, rows, base, selection, staleKeys,
}: {
  groups: BacklogGroup[];
  epics: BacklogEpic[];
  rows: BacklogRow[];
  base: string;
  selection: Selection;
  staleKeys: Set<string>;
}) {
  const byKey = new Map((rows ?? []).map((r) => [r.key, r]));
  return (
    <>
      {(groups ?? []).map((g) => (
        <GroupCard key={g.id} group={g} byKey={byKey} base={base} selection={selection} staleKeys={staleKeys} />
      ))}
      <Section
        title="Epics"
        hint="Every epic with open work under it, and how much of that work is still to be refined. Selecting the unrefined ones puts them in the bulk bar."
      >
        <Epics epics={epics ?? []} base={base} selection={selection} />
      </Section>
    </>
  );
}
