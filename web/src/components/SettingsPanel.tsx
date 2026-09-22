import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  fetchConventions, fetchPeople, fetchTeamImport, savePerson,
  type SprintReport, type StoredPerson, type TeamCandidate,
} from "../api";
import { NumberField, SaveBar, SidePanel, parse, stateOf } from "./Panel";

/* Settings: the half of the sprint report that is not about a sprint.
 *
 * The report page used to hold both, and nobody could tell them apart.
 * Who is on the team and what each of them is expected to deliver changes
 * when somebody joins or goes part-time - a few times a year. Who was
 * away changes every fortnight. Putting them in one table made the
 * durable thing look like something you were being asked to fill in
 * again, so it now lives behind the gear and the sprint page keeps only
 * the sprint.
 *
 * THE THREE SETS OF PEOPLE.
 *
 * The confusion this panel exists to end is that there are three
 * different groups here and they are all legitimately different sizes:
 *
 *   The Atlassian team is who Atlassian says is on the team. It is other
 *   people's data and Argus only reads it.
 *
 *   The roster is who Argus knows about. It is the only one of the three
 *   Argus owns, and the only one that can hold a baseline.
 *
 *   Whoever delivered work in the sprint. Someone on the team can deliver
 *   nothing; someone who is not on the team - a contractor, another
 *   team's engineer - can deliver a great deal.
 *
 * So the table below is the union of all three, and each row says which
 * of them a person is in. The tick is the only thing that changes any of
 * it: it is the opt-in, and it decides whether the report measures this
 * person against a baseline. It is not a delete and it is not a filter on
 * delivery - see the note beneath the table, which says so on the screen
 * rather than only here. */

type Edit = {
  /** Undefined means unchanged. A blank string is a cleared field, which
   *  Save refuses rather than reading as zero. */
  baseline?: string;
  active?: boolean;
};

/** Row is one person, assembled from whichever of the three sets knows
 *  about them. */
type Row = {
  accountID: string;
  name: string;
  stored?: StoredPerson;
  candidate?: TeamCandidate;
  delivered?: number;
};

function Tag({ label, tone = "muted", title }: { label: string; tone?: "muted" | "good" | "warn"; title?: string }) {
  const colour = tone === "good" ? "var(--good)" : tone === "warn" ? "var(--warn)" : "var(--faint)";
  const background = tone === "good" ? "var(--good-bg)" : tone === "warn" ? "var(--warn-bg)" : "var(--surface-2)";
  return (
    <span
      title={title}
      className="rounded px-1.5 py-px text-[10.5px] font-medium whitespace-nowrap"
      style={{ background, color: colour }}
    >
      {label}
    </span>
  );
}

export function SettingsPanel({
  report, onClose,
}: {
  /** The sprint currently on screen, when there is one. It is only used
   *  to show who delivered work, so the panel opens and works without it. */
  report?: SprintReport;
  onClose: () => void;
}) {
  const qc = useQueryClient();
  const [edits, setEdits] = useState<Record<string, Edit>>({});

  const conv = useQuery({ queryKey: ["conventions"], queryFn: fetchConventions, staleTime: Infinity });
  const roster = useQuery({ queryKey: ["people"], queryFn: fetchPeople });

  // Asked for as the panel opens rather than behind the Import button.
  // The counts only make sense side by side, and an install with no
  // Atlassian team configured answers this immediately and without
  // reaching Atlassian at all.
  const team = useQuery({ queryKey: ["team-import"], queryFn: fetchTeamImport, staleTime: 5 * 60_000 });

  const hoursPerPoint = conv.data?.hours_per_point ?? 6;
  const hoursPerDay = conv.data?.hours_per_day ?? 6;
  const sprintDays = conv.data?.sprint_length_days ?? 10;
  const daysPerPoint = hoursPerPoint / hoursPerDay;

  const rows = useMemo<Row[]>(() => {
    const byID = new Map<string, Row>();
    const put = (id: string, patch: Partial<Row> & { name?: string }) => {
      const existing = byID.get(id) ?? { accountID: id, name: "" };
      byID.set(id, { ...existing, ...patch, name: patch.name || existing.name });
    };

    for (const p of roster.data?.people ?? []) put(p.account_id, { name: p.name, stored: p });
    for (const c of team.data?.candidates ?? []) put(c.account_id, { name: c.name, candidate: c });
    for (const p of report?.people ?? []) {
      if (p.delivered > 0 || !p.on_roster) put(p.account_id, { name: p.name, delivered: p.delivered });
    }

    return [...byID.values()].sort((a, b) => {
      // Whoever Argus already knows about first: that is the list being
      // maintained. Everyone else is a proposal beneath it.
      const known = Number(Boolean(b.stored)) - Number(Boolean(a.stored));
      return known !== 0 ? known : a.name.localeCompare(b.name);
    });
  }, [roster.data, team.data, report]);

  const optedIn = (r: Row) => edits[r.accountID]?.active ?? r.stored?.active ?? false;
  const baselineOf = (r: Row) => r.stored?.baseline ?? 0;
  const draftOf = (r: Row) => edits[r.accountID]?.baseline;

  const setActive = (r: Row, value: boolean) => {
    setEdits((prev) => {
      const next = { ...prev };
      const entry = { ...(next[r.accountID] ?? {}) };
      if (value === (r.stored?.active ?? false)) delete entry.active;
      else entry.active = value;
      if (entry.baseline === undefined && entry.active === undefined) delete next[r.accountID];
      else next[r.accountID] = entry;
      return next;
    });
  };

  const setBaseline = (r: Row, raw: string) => {
    setEdits((prev) => {
      const next = { ...prev };
      const entry = { ...(next[r.accountID] ?? {}) };
      // Typing the stored value back drops the draft, so "unsaved
      // changes" never counts a change that undid itself.
      if (parse(raw) === baselineOf(r)) delete entry.baseline;
      else entry.baseline = raw;
      if (entry.baseline === undefined && entry.active === undefined) delete next[r.accountID];
      else next[r.accountID] = entry;
      return next;
    });
  };

  const changed = rows.filter((r) => edits[r.accountID] !== undefined);
  const invalid = changed.some((r) => {
    const d = draftOf(r);
    return d !== undefined && Number.isNaN(parse(d));
  });

  const save = useMutation({
    mutationFn: async () => {
      for (const r of changed) {
        const d = draftOf(r);
        await savePerson({
          account_id: r.accountID,
          name: r.name,
          baseline: d !== undefined ? parse(d) : baselineOf(r),
          active: optedIn(r),
        });
      }
    },
    onSuccess: () => {
      setEdits({});
      qc.invalidateQueries({ queryKey: ["people"] });
      qc.invalidateQueries({ queryKey: ["team-import"] });
      // A baseline is an input to every report, so the ones already built
      // are now wrong. The server folds the change in before it answers,
      // so this refetch comes back with the new figures.
      qc.invalidateQueries({ queryKey: ["sprint-report"] });
      qc.invalidateQueries({ queryKey: ["sprints"] });
    },
  });

  // ---- the import -----------------------------------------------------

  const imported = team.data;
  const newMembers = (imported?.candidates ?? []).filter(
    (c) => c.state === "new" && c.suggested && !edits[c.account_id]?.active,
  );
  const departed = (imported?.candidates ?? []).filter((c) => c.state === "departed");

  const tickNewMembers = () => {
    setEdits((prev) => {
      const next = { ...prev };
      for (const c of newMembers) {
        next[c.account_id] = { ...(next[c.account_id] ?? {}), active: true };
      }
      return next;
    });
  };

  const teamCount = imported?.configured ? (imported.candidates ?? []).filter((c) => c.state !== "departed").length : null;
  const rosterCount = (roster.data?.people ?? []).length;
  const rosterOptedIn = (roster.data?.people ?? []).filter((p) => p.active).length;
  const deliveredCount = (report?.people ?? []).filter((p) => p.delivered > 0).length;

  return (
    <SidePanel
      title="Settings"
      subtitle="Who is on the team, and what each of them is expected to deliver. This carries into every sprint."
      pending={changed.length > 0}
      onClose={onClose}
      footer={
        <SaveBar
          dirty={changed.length}
          invalid={invalid}
          state={stateOf(save)}
          error={save.error instanceof Error ? save.error.message : undefined}
          onSave={() => save.mutate()}
        />
      }
    >
      <div
        className="mb-4 rounded-lg px-3 py-2 text-[12.5px]"
        style={{ background: "var(--surface-2)", color: "var(--muted)" }}
      >
        Your team&rsquo;s convention: <strong style={{ color: "var(--ink)" }}>1 point = {hoursPerPoint}h</strong>
        {daysPerPoint === 1 ? " = 1 working day" : ` = ${daysPerPoint.toFixed(2)} working days`}, and a
        sprint is <strong style={{ color: "var(--ink)" }}>{sprintDays} working days</strong>.
        {" "}A full-time person is therefore about {(sprintDays / daysPerPoint).toFixed(0)} points.
      </div>

      {/* The three counts, together, because apart they look like a bug.
          A team of ten with a roster of eight and nine people delivering
          is three correct numbers about three different questions. */}
      <div className="mb-3 flex flex-wrap items-center gap-x-4 gap-y-1 text-[12.5px]" style={{ color: "var(--muted)" }}>
        {teamCount !== null && (
          <span>
            Atlassian team <strong style={{ color: "var(--ink)" }}>{teamCount}</strong>
          </span>
        )}
        <span>
          Roster <strong style={{ color: "var(--ink)" }}>{rosterCount}</strong> · opted in{" "}
          <strong style={{ color: "var(--ink)" }}>{rosterOptedIn}</strong>
        </span>
        {report && (
          <span>
            Delivered in sprint {report.sprint.number}{" "}
            <strong style={{ color: "var(--ink)" }}>{deliveredCount}</strong>
          </span>
        )}
      </div>

      {/* ---- import ---- */}
      <div
        className="mb-4 rounded-lg px-3 py-2.5"
        style={{ background: "var(--surface-2)" }}
      >
        {team.isLoading && (
          <p className="text-[12.5px]" style={{ color: "var(--muted)" }}>Asking Atlassian who is on the team…</p>
        )}

        {team.error && (
          <p className="text-[12.5px]" style={{ color: "var(--crit)" }}>
            Could not read the Atlassian team: {String(team.error)}
          </p>
        )}

        {imported && !imported.configured && (
          // Not a dead button. Somebody who has not set this up should be
          // told what to set, not left pressing something that fails.
          <p className="text-[12.5px]" style={{ color: "var(--muted)" }}>
            <strong style={{ color: "var(--ink)" }}>Importing the roster is not set up.</strong>{" "}
            {imported.reason}
          </p>
        )}

        {imported?.configured && (
          <div className="flex flex-wrap items-center gap-2">
            <span className="flex-1 text-[12.5px]" style={{ color: "var(--muted)" }}>
              {newMembers.length > 0
                ? `${newMembers.length} ${newMembers.length === 1 ? "person is" : "people are"} on ${imported.team_name || "the Atlassian team"} and not yet on the roster.`
                : `Everyone on ${imported.team_name || "the Atlassian team"} is already on the roster.`}
            </span>
            {newMembers.length > 0 && (
              <button
                onClick={tickNewMembers}
                className="rounded-lg px-3 py-1.5 text-[13px] font-medium"
                style={{ background: "var(--surface)", border: "1px solid var(--line)", color: "var(--ink)" }}
              >
                Import {newMembers.length}
              </button>
            )}
            <button
              onClick={() => team.refetch()}
              disabled={team.isFetching}
              className="rounded-lg px-2.5 py-1.5 text-[12.5px] disabled:opacity-50"
              style={{ border: "1px solid var(--line)", color: "var(--muted)" }}
            >
              {team.isFetching ? "Checking…" : "Recheck"}
            </button>
          </div>
        )}

        {departed.length > 0 && (
          <p className="mt-2 text-[12.5px]" style={{ color: "var(--warn)" }}>
            {departed.map((c) => c.name).join(", ")}{" "}
            {departed.length === 1 ? "is" : "are"} on the roster but no longer on the Atlassian team. Nothing has
            been removed — what they delivered still happened. Untick them below to stop measuring them.
          </p>
        )}
      </div>

      {/* ---- the roster ---- */}
      {rows.length === 0 && !roster.isLoading && (
        <p className="py-6 text-center text-[13px]" style={{ color: "var(--faint)" }}>
          Nobody on the roster yet.
        </p>
      )}

      <table className="w-full border-collapse">
        <thead>
          <tr>
            <th className="pb-2 text-left text-[11px] font-semibold tracking-wide uppercase" style={{ color: "var(--faint)" }}>
              Measured
            </th>
            <th className="pb-2 text-left text-[11px] font-semibold tracking-wide uppercase" style={{ color: "var(--faint)" }}>
              Person
            </th>
            <th className="pb-2 text-right text-[11px] font-semibold tracking-wide uppercase" style={{ color: "var(--faint)" }}>
              Baseline
            </th>
            <th className="pb-2 text-right text-[11px] font-semibold tracking-wide uppercase" style={{ color: "var(--faint)" }}>
              Which is
            </th>
          </tr>
        </thead>
        <tbody>
          {rows.map((r) => {
            const on = optedIn(r);
            const draft = draftOf(r);
            const shown = draft !== undefined ? parse(draft) : baselineOf(r);
            const changedRow = edits[r.accountID] !== undefined;
            return (
              <tr key={r.accountID} className="border-t" style={{ borderColor: "var(--line)" }}>
                <td className="py-2.5 pr-2 align-top">
                  <input
                    type="checkbox"
                    checked={on}
                    aria-label={`Measure ${r.name} against a baseline`}
                    onChange={(e) => setActive(r, e.target.checked)}
                    className="mt-1 h-4 w-4 accent-[var(--accent)]"
                  />
                </td>
                <td className="py-2.5 pr-3 text-[13px]">
                  <span style={{ fontWeight: changedRow ? 600 : 400 }}>{r.name}</span>
                  <div className="mt-0.5 flex flex-wrap items-center gap-1">
                    {r.candidate && r.candidate.state !== "departed" && (
                      <Tag label="Atlassian team" tone="good" title="Atlassian says this person is on the team." />
                    )}
                    {r.candidate?.state === "departed" && (
                      <Tag label="left the team" tone="warn" title="On the roster, but Atlassian no longer lists them on the team." />
                    )}
                    {imported?.configured && !r.candidate && (
                      <Tag label="not on the Atlassian team" title="Delivers work here without being on the team - a contractor, or another team." />
                    )}
                    {!r.stored && <Tag label="not on the roster yet" title="Ticking and saving adds them." />}
                    {r.delivered !== undefined && r.delivered > 0 && (
                      <Tag label={`delivered ${r.delivered.toFixed(1)} pts`} />
                    )}
                    {r.candidate && !r.candidate.account_active && (
                      <Tag label="account deactivated" tone="warn" />
                    )}
                    {r.candidate && !r.candidate.human && <Tag label="app account" tone="warn" />}
                  </div>
                </td>
                <td className="py-2.5 text-right align-top">
                  <NumberField
                    label={`Baseline for ${r.name}`}
                    value={baselineOf(r)}
                    draft={draft}
                    disabled={!on}
                    onChange={(s) => setBaseline(r, s)}
                  />
                  <span className="ml-1 text-[11px]" style={{ color: "var(--faint)" }}>pts</span>
                </td>
                <td className="py-2.5 text-right align-top text-[12px]" style={{ color: "var(--muted)" }}>
                  {on && shown > 0 && !Number.isNaN(shown)
                    ? `${(shown * daysPerPoint).toFixed(1)} days · ${(shown * hoursPerPoint).toFixed(0)}h`
                    : "—"}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>

      {/* What the tick does, and just as importantly what it does not. */}
      <div className="mt-4 space-y-1.5 text-[12px]" style={{ color: "var(--faint)" }}>
        <p>
          <strong style={{ color: "var(--muted)" }}>Ticked</strong> means measured: this person has a baseline, gets a
          capacity line each sprint, and counts towards the team&rsquo;s capacity total.
        </p>
        <p>
          <strong style={{ color: "var(--muted)" }}>Unticked</strong> means known and not measured. Points they
          delivered still count towards the sprint total and their tickets are still listed — they are simply not
          compared against a baseline they do not have. Nothing is deleted, and old reports still name them.
        </p>
      </div>
    </SidePanel>
  );
}
