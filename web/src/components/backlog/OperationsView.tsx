import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { fetchOpsRoster, saveOpsRoster, type Backlog, type BacklogRow, type OpsMember } from "../../api";
import { outlined, small } from "../closeout/Table";
import { SaveBar, stateOf } from "../Panel";
import type { BatchRequest } from "./BatchPanel";
import { Picker } from "./Picker";
import { Rows, type Selection } from "./Rows";
import { Section } from "./Section";

/* Operations: the requests from the operations team, and what marks
 * them as such.
 *
 * A request is one of three things: labelled with the current label,
 * labelled with a legacy one, or reported by somebody on the roster.
 * The roster is kept here, like the sprint roster, and edited the same
 * way - typing does not save, Save saves. Under it, the two fixes each
 * with a one-click batch, then every request together. Only the current
 * label is ever added; the legacy label is replaced, never written, so
 * it dies out. */

function Roster() {
  const qc = useQueryClient();
  const roster = useQuery({ queryKey: ["ops-roster"], queryFn: fetchOpsRoster });
  // Null means "as the server has it"; a list means edits not yet saved.
  const [draft, setDraft] = useState<OpsMember[] | null>(null);
  const members = draft ?? roster.data?.members ?? [];
  const save = useMutation({
    mutationFn: () => saveOpsRoster(members),
    onSuccess: () => {
      setDraft(null);
      qc.invalidateQueries({ queryKey: ["ops-roster"] });
      // The roster decides which reporters count, so the rows move too.
      qc.invalidateQueries({ queryKey: ["backlog"] });
    },
  });

  const onRoster = new Set(members.map((m) => m.account_id));
  const candidates = (roster.data?.candidates ?? [])
    .filter((c) => !onRoster.has(c.account_id))
    .map((c) => ({ id: c.account_id, label: c.label, hint: `${c.reported} reported` }));
  const add = (id: string) => {
    const c = (roster.data?.candidates ?? []).find((x) => x.account_id === id);
    if (c) setDraft([...members, { account_id: c.account_id, name: c.label }]);
  };
  const remove = (id: string) => setDraft(members.filter((m) => m.account_id !== id));

  // Unsaved changes, counted as people added plus people removed, so the
  // note under the list says how many rather than only that there are some.
  const saved = roster.data?.members ?? [];
  const dirty = draft === null ? 0
    : draft.filter((m) => !saved.some((s) => s.account_id === m.account_id)).length
      + saved.filter((s) => !draft.some((m) => m.account_id === s.account_id)).length;

  return (
    <Section
      title="Operations roster"
      hint="Whose tickets count as requests even without the label. Add people from whoever has reported open tickets; nothing is saved until Save."
      right={<Picker label="Add from reporters" items={candidates} onPick={add} placeholder="Find a reporter…" disabled={roster.isLoading} />}
    >
      {roster.error && (
        <p className="px-4 py-2 text-[13px]" style={{ background: "var(--crit-bg)", color: "var(--crit)" }}>
          {roster.error instanceof Error ? roster.error.message : String(roster.error)}
        </p>
      )}
      {members.length === 0 ? (
        <p className="px-4 py-6 text-center text-sm" style={{ color: "var(--faint)" }}>
          {roster.isLoading ? "Reading the roster…" : "Nobody on the roster. Requests are then found by label alone."}
        </p>
      ) : (
        <ul className="divide-y" style={{ borderColor: "var(--line)" }}>
          {members.map((m) => (
            <li key={m.account_id} className="flex items-center gap-2 px-4 py-2 text-[13px]">
              <span className="flex-1">{m.name}</span>
              <button onClick={() => remove(m.account_id)} className={small} style={outlined}>Remove</button>
            </li>
          ))}
        </ul>
      )}
      <SaveBar
        dirty={dirty}
        invalid={false}
        state={stateOf(save)}
        error={save.error instanceof Error ? save.error.message : undefined}
        onSave={() => save.mutate()}
      />
    </Section>
  );
}

export function OperationsView({
  data, selection, staleKeys, onBatch,
}: {
  data: Backlog;
  selection: Selection;
  staleKeys: Set<string>;
  onBatch: (req: BatchRequest) => void;
}) {
  const byKey = new Map((data.rows ?? []).map((r) => [r.key, r]));
  const pick = (keys: string[] | undefined) =>
    (keys ?? []).map((k) => byKey.get(k)).filter((r): r is BacklogRow => !!r);
  const ops = data.operations;
  const label = data.settings?.operations_label || "Operations";
  const legacy = (data.settings?.legacy_labels ?? []).join(", ") || "the legacy label";
  const missing = ops?.missing_label_keys ?? [];
  const old = ops?.legacy_label_keys ?? [];
  const all = ops?.all_keys ?? [];

  return (
    <>
      <Roster />

      <Section
        title={`Missing the label · ${missing.length}`}
        hint={`Reported by somebody on the roster but not labelled ${label}. One click previews adding it to all of them.`}
        right={
          <button
            onClick={() => onBatch({ action: "operations.label", keys: missing, params: {}, label: `Add ${label}` })}
            disabled={missing.length === 0}
            className={`${small} font-medium`}
            style={{ background: "var(--accent)", color: "#fff" }}
          >
            Add {label} to these
          </button>
        }
      >
        <Rows rows={pick(missing)} selection={selection} staleKeys={staleKeys} empty="Every roster request carries the label." />
      </Section>

      <Section
        title={`Legacy label · ${old.length}`}
        hint={`Labelled ${legacy} rather than ${label}. Replacing it is how the old label dies out; it is read as the same thing until then.`}
        right={
          <button
            onClick={() => onBatch({ action: "operations.migrate", keys: old, params: {}, label: `Replace ${legacy} with ${label}` })}
            disabled={old.length === 0}
            className={`${small} font-medium`}
            style={{ background: "var(--accent)", color: "#fff" }}
          >
            Replace {legacy} with {label}
          </button>
        }
      >
        <Rows rows={pick(old)} selection={selection} staleKeys={staleKeys} empty="Nothing carries the legacy label." />
      </Section>

      <Section
        title={`All requests · ${all.length}`}
        hint="Every open ticket that is an Operations request, by label, legacy label or roster reporter."
      >
        <Rows rows={pick(all)} selection={selection} staleKeys={staleKeys} empty="No open requests." />
      </Section>
    </>
  );
}
