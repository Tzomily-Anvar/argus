import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { fetchLabels, fetchRequesters, fetchRequestTeam, saveLabels, saveRequesters, type Requester } from "../../api";
import { outlined, small } from "../closeout/Table";
import { Notice, errorText } from "../Notice";
import { SaveBar, SidePanel, stateOf } from "../Panel";
import { Picker } from "./Picker";
import type { ReactNode } from "react";

/* The Backlog tool's settings: the requesters, and the labels the bulk
 * bar offers.
 *
 * The requesters are the people whose tickets count as requests from
 * outside the team even without the label - typically an operations or
 * support team whose tickets engineering triages, though the tool does
 * not assume so. Behind the gear, where the sprint report keeps its
 * team, and for the same reason: who asks and which labels the team
 * applies change a few times a year, while the tickets change daily.
 * Keeping the list on the Requests view made the durable thing look
 * like part of the day's work. Edited the same way as the sprint roster
 * - typing does not save, Save saves - and the panel refuses to close on
 * unsaved changes without saying so.
 *
 * Two sources feed the list: the Atlassian team, when one is
 * configured, which proposes its members; and whoever has reported an
 * open ticket, so the list can be built from who actually asks rather
 * than typed from memory. The labels are picked from what the open
 * backlog carries, most used first, or typed for one not in use yet.
 * All of it only proposes; nothing is saved until Save. */

export function BacklogSettingsPanel({ onClose }: { onClose: () => void }) {
  const qc = useQueryClient();
  const labels = useLabels();
  const requesters = useQuery({ queryKey: ["requesters"], queryFn: fetchRequesters });
  // Null means "as the server has it"; a list means edits not yet saved.
  const [draft, setDraft] = useState<Requester[] | null>(null);
  const members = draft ?? requesters.data?.requesters ?? [];
  // One Save for both lists; each is only written when it changed.
  const save = useMutation({
    mutationFn: async () => {
      if (draft !== null) await saveRequesters(members);
      if (labels.draft !== null) await saveLabels(labels.chosen);
    },
    onSuccess: () => {
      setDraft(null);
      labels.setDraft(null);
      qc.invalidateQueries({ queryKey: ["requesters"] });
      qc.invalidateQueries({ queryKey: ["labels"] });
      // The requesters decide which reporters count, so the rows move too.
      qc.invalidateQueries({ queryKey: ["backlog"] });
    },
  });

  const listed = new Set(members.map((m) => m.account_id));
  const candidates = (requesters.data?.candidates ?? [])
    .filter((c) => !listed.has(c.account_id))
    .map((c) => ({ id: c.account_id, label: c.label, hint: `${c.reported} reported` }));
  const add = (id: string) => {
    const c = (requesters.data?.candidates ?? []).find((x) => x.account_id === id);
    if (c) setDraft([...members, { account_id: c.account_id, name: c.label }]);
  };
  const remove = (id: string) => setDraft(members.filter((m) => m.account_id !== id));

  const team = useQuery({ queryKey: ["request-team"], queryFn: fetchRequestTeam, staleTime: 60_000 });
  const proposed = (team.data?.candidates ?? []).filter((c) => !listed.has(c.account_id));
  const importTeam = () => {
    if (proposed.length === 0) return;
    setDraft([...members, ...proposed.map((c) => ({ account_id: c.account_id, name: c.name }))]);
  };

  // Unsaved changes, counted as people added plus people removed, so the
  // note under the list says how many rather than only that there are some.
  const saved = requesters.data?.requesters ?? [];
  const dirty = (draft === null ? 0
    : draft.filter((m) => !saved.some((s) => s.account_id === m.account_id)).length
      + saved.filter((s) => !draft.some((m) => m.account_id === s.account_id)).length)
    + labels.dirty;

  return (
    <SidePanel
      title="Settings"
      subtitle="The requesters, and the labels the bulk bar offers. Both carry across every sweep."
      pending={dirty > 0}
      onClose={onClose}
      footer={
        <SaveBar
          dirty={dirty}
          invalid={false}
          state={stateOf(save)}
          error={save.error instanceof Error ? save.error.message : undefined}
          onSave={() => save.mutate()}
        />
      }
    >
      <ListBlock
        title="Requesters"
        controls={
          <>
            {team.data?.configured && (
              <button
                onClick={importTeam}
                disabled={proposed.length === 0}
                title={proposed.length === 0
                  ? `Everyone on ${team.data.team_name ?? "the team"} is already a requester.`
                  : `Add the ${proposed.length} on ${team.data.team_name ?? "the team"} who are not requesters yet.`}
                className={small}
                style={outlined}
              >
                Import from {team.data.team_name ?? "Atlassian team"}{proposed.length > 0 ? ` (${proposed.length})` : ""}
              </button>
            )}
            <Picker label="Add from reporters" items={candidates} onPick={add} placeholder="Find a reporter…" disabled={requesters.isLoading} />
          </>
        }
        error={requesters.error}
        items={members.map((m) => ({ id: m.account_id, label: m.name }))}
        onRemove={remove}
        empty={requesters.isLoading ? "Reading the requesters…" : "No requesters. Requests are then found by label alone."}
        note={
          <>
            Requesters: whose tickets count as requests from outside the team even without the label.
            The Requests view offers to add the label to them. The list is Argus's own; the Atlassian
            team only proposes members, and removing somebody here changes nothing in Atlassian.
          </>
        }
      />

      <LabelsBlock labels={labels} />
    </SidePanel>
  );
}

/* One editable list: a heading with the controls that add to it, a
 * bordered list with a Remove on every row, what to say when it is
 * empty, and a sentence under it saying what the list is for. The
 * requesters and the labels are the same shape, and look it. */
function ListBlock({
  title, controls, error, items, mono = false, onRemove, empty, note,
}: {
  title: string;
  controls: ReactNode;
  error: unknown;
  items: { id: string; label: string }[];
  /** Labels are set in monospace, as they are in the rows. */
  mono?: boolean;
  onRemove: (id: string) => void;
  empty: string;
  note: ReactNode;
}) {
  return (
    <>
      <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
        <h3 className="text-[13px] font-semibold">{title}</h3>
        <span className="flex items-center gap-2">{controls}</span>
      </div>

      {!!error && <Notice layout="panel" className="mb-3">{errorText(error)}</Notice>}

      <div className="rounded-xl" style={{ border: "1px solid var(--line)" }}>
        {items.length === 0 ? (
          <p className="px-4 py-6 text-center text-sm" style={{ color: "var(--faint)" }}>{empty}</p>
        ) : (
          <ul className="divide-y" style={{ borderColor: "var(--line)" }}>
            {items.map((i) => (
              <li key={i.id} className="flex items-center gap-2 px-4 py-2 text-[13px]">
                <span className={mono ? "flex-1 font-mono" : "flex-1"}>{i.label}</span>
                <button onClick={() => onRemove(i.id)} className={small} style={outlined}>Remove</button>
              </li>
            ))}
          </ul>
        )}
      </div>

      <p className="mt-3 text-[12.5px]" style={{ color: "var(--muted)" }}>{note}</p>
    </>
  );
}

/* The labels block: what the bulk bar's "Add label" and "Remove label"
 * pickers offer. Picked from the labels the open backlog carries, or
 * typed for one not in use yet; Jira creates a label on first use, so a
 * typed one is not a mistake, only a spelling to check. */

function useLabels() {
  const query = useQuery({ queryKey: ["labels"], queryFn: fetchLabels });
  const [draft, setDraft] = useState<string[] | null>(null);
  const saved = (query.data?.labels ?? []).map((l) => l.name);
  const chosen = draft ?? saved;
  const dirty = draft === null ? 0
    : draft.filter((l) => !saved.includes(l)).length + saved.filter((l) => !draft.includes(l)).length;
  return { query, draft, setDraft, chosen, dirty };
}

function LabelsBlock({ labels }: { labels: ReturnType<typeof useLabels> }) {
  const [typed, setTyped] = useState("");
  const { query, chosen, setDraft } = labels;
  const inUse = (query.data?.in_use ?? [])
    .filter((l) => !chosen.includes(l.name))
    .map((l) => ({ id: l.name, label: l.name, hint: `${l.count} ${l.count === 1 ? "ticket" : "tickets"}` }));
  const add = (name: string) => {
    const clean = name.trim();
    if (!clean || /\s/.test(clean) || chosen.includes(clean)) return;
    setDraft([...chosen, clean]);
    setTyped("");
  };
  const remove = (name: string) => setDraft(chosen.filter((l) => l !== name));

  return (
    <div className="mt-6">
      <ListBlock
        title="Labels"
        controls={
          <>
            <Picker label="Add from labels in use" items={inUse} onPick={add} placeholder="Find a label…" disabled={query.isLoading} />
            <input
              value={typed}
              onChange={(e) => setTyped(e.target.value)}
              onKeyDown={(e) => { if (e.key === "Enter") add(typed); }}
              placeholder="or type one"
              aria-label="A label to add"
              className="w-32 rounded-lg px-2.5 py-1.5 text-[13px] outline-none"
              style={{ background: "var(--surface-2)", border: "1px solid var(--line)", color: "var(--ink)" }}
            />
            <button onClick={() => add(typed)} disabled={!typed.trim() || /\s/.test(typed.trim())} className={small} style={outlined}>Add</button>
          </>
        }
        error={query.error}
        items={chosen.map((l) => ({ id: l, label: l }))}
        mono
        onRemove={remove}
        empty={query.isLoading ? "Reading the labels…" : "No labels chosen. The bulk bar then asks you to type one each time."}
        note="The bulk bar offers these when adding or removing labels, so a label is picked rather than spelled. A label is one word. Removing one here only takes it off the list; the tickets keep it."
      />
    </div>
  );
}
