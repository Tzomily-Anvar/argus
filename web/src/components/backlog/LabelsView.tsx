import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { fetchLabels, type Backlog } from "../../api";
import { outlined, small } from "../closeout/Table";
import type { BatchRequest } from "./BatchPanel";
import { Picker } from "./Picker";
import { Rows, type Selection } from "./Rows";
import { Section, scopeOf } from "./Section";

/* Labels: the open backlog by one label at a time.
 *
 * A team's labels drift - a spelling changes, two mean the same thing,
 * one marks a kind of work nobody does any more - and tidying them means
 * seeing every ticket under one label and moving or dropping the lot.
 * The picker is every label the open backlog carries with its count,
 * most used first, as the server counts them; the rows are the tickets
 * under the chosen one; the two buttons act on the ticked rows of that
 * list or on all of it, and say which. A migration is the same batch
 * the Requests view uses to retire a legacy label, with any label as
 * what goes and any as what comes. */

export function LabelsView({
  data, selection, staleKeys, onBatch,
}: {
  data: Backlog;
  selection: Selection;
  staleKeys: Set<string>;
  onBatch: (req: BatchRequest) => void;
}) {
  const labels = useQuery({ queryKey: ["labels"], queryFn: fetchLabels, staleTime: 60_000 });
  const inUse = labels.data?.in_use ?? [];
  const chosen = (labels.data?.labels ?? []).map((l) => l.name);
  const [label, setLabel] = useState<string | null>(null);
  // A label that left the backlog since it was picked (its last ticket
  // migrated, say) is still shown, with nothing under it, rather than
  // silently swapped for another.
  const count = inUse.find((l) => l.name === label)?.count ?? 0;
  const rows = label ? (data.rows ?? []).filter((r) => (r.labels ?? []).includes(label)) : [];
  const scope = scopeOf(rows.map((r) => r.key), selection);
  // Where a migration can go: the labels chosen behind the gear and the
  // ones in use, less the one being replaced.
  const targets = [...new Set([...chosen, ...inUse.map((l) => l.name)])]
    .filter((l) => l !== label)
    .map((l) => ({ id: l, label: l, hint: chosen.includes(l) ? "chosen" : undefined }));

  return (
    <Section
      title={label ? `${label} · ${count}` : "Labels"}
      hint={label
        ? `Every open ticket labelled ${label}. Migrate moves them all to another label in one edit each; Remove takes the label off and leaves the rest.`
        : "Pick a label to see every open ticket carrying it, then move them to another label or take it off them."}
      right={
        <span className="flex items-center gap-2">
          <Picker
            label={label ? "Another label" : "Pick a label"}
            items={inUse.map((l) => ({ id: l.name, label: l.name, hint: `${l.count} ${l.count === 1 ? "ticket" : "tickets"}` }))}
            onPick={setLabel}
            placeholder="Find a label…"
            disabled={labels.isLoading}
          />
          {label && (
            <>
              <Picker
                label={`Migrate to… · ${scope.count}`}
                items={targets}
                onPick={(to) => onBatch({
                  action: "labels.migrate", keys: scope.keys, params: { from: [label], to }, label: `Migrate ${label} to ${to}`,
                })}
                placeholder="Find the new label…"
                disabled={scope.keys.length === 0}
              />
              <button
                onClick={() => onBatch({ action: "labels.remove", keys: scope.keys, params: { labels: [label] }, label: `Remove ${label}` })}
                disabled={scope.keys.length === 0}
                className={small}
                style={outlined}
              >
                Remove {label} · {scope.count}
              </button>
            </>
          )}
        </span>
      }
    >
      {labels.error && (
        <p className="px-4 py-3 text-[13px]" style={{ background: "var(--crit-bg)", color: "var(--crit)" }}>
          {labels.error instanceof Error ? labels.error.message : String(labels.error)}
        </p>
      )}
      {label ? (
        <Rows rows={rows} selection={selection} staleKeys={staleKeys} empty={`Nothing open carries ${label} any more.`} />
      ) : (
        <p className="px-4 py-6 text-center text-sm" style={{ color: "var(--faint)" }}>
          {labels.isLoading ? "Reading the labels…" : inUse.length === 0 ? "No open ticket carries a label." : `${inUse.length} ${inUse.length === 1 ? "label" : "labels"} in use. Pick one above.`}
        </p>
      )}
    </Section>
  );
}
