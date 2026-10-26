import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { fetchLabels, type Backlog, type BacklogLabels } from "../../api";
import { labelItems, newLabelItems, STYLE_NAMES } from "../../labels";
import { outlined, small } from "../closeout/Table";
import { errorText } from "../Notice";
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
 * what goes and any as what comes.
 *
 * With no label picked, the view lists the labels to tidy: one label
 * spelt more than one way, and, where the team has named a spelling, a
 * label spelt otherwise, each with the migration that settles it. */

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
  const [label, setLabel] = useState<string | null>(null);
  // A label that left the backlog since it was picked (its last ticket
  // migrated, say) is still shown, with nothing under it, rather than
  // silently swapped for another.
  const count = inUse.find((l) => l.name === label)?.count ?? 0;
  const rows = label ? (data.rows ?? []).filter((r) => (r.labels ?? []).includes(label)) : [];
  const scope = scopeOf(rows.map((r) => r.key), selection);
  // Where a migration can go: any label offered but the one being
  // replaced, or a new one in the team's spelling.
  const targets = labelItems(labels.data, label ? [label] : []);
  const known = labelItems(labels.data, label ? [label] : []).map((i) => i.id);
  const create = (q: string) => newLabelItems(q, known, labels.data?.style ?? "any");

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
                extra={create}
                onPick={(to) => onBatch({
                  action: "labels.migrate", keys: scope.keys, params: { from: [label], to }, label: `Migrate ${label} to ${to}`,
                })}
                placeholder="Find or type the new label…"
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
          {errorText(labels.error)}
        </p>
      )}
      {label ? (
        <Rows rows={rows} selection={selection} staleKeys={staleKeys} empty={`Nothing open carries ${label} any more.`} />
      ) : (
        <>
          <p className="px-4 py-6 text-center text-sm" style={{ color: "var(--faint)" }}>
            {labels.isLoading ? "Reading the labels…" : inUse.length === 0 ? "No open ticket carries a label." : `${inUse.length} ${inUse.length === 1 ? "label" : "labels"} in use. Pick one above.`}
          </p>
          {labels.data && <Spelling data={data} labels={labels.data} onPick={setLabel} onBatch={onBatch} />}
        </>
      )}
    </Section>
  );
}

/* The labels to tidy. Each line is the labels to replace, the spelling
 * to keep, and one button that migrates every open ticket carrying any
 * of them; a label's name opens its tickets above. Under it, while the
 * team has named no spelling, how the labels in use split today, as the
 * evidence to choose one by - it is reported, never applied. */
function Spelling({
  data, labels, onPick, onBatch,
}: {
  data: Backlog;
  labels: BacklogLabels;
  onPick: (label: string) => void;
  onBatch: (req: BatchRequest) => void;
}) {
  const findings = labels.spelling ?? [];
  const style = labels.style ?? "any";
  const t = labels.tally;
  const split = t && (["train", "kebab", "snake", "camel", "pascal"] as const)
    .filter((k) => t[k] > 0)
    .map((k) => `${t[k]} ${STYLE_NAMES[k]}`)
    .concat(t.other > 0 ? [`${t.other} mixed`] : []);

  return (
    <div className="border-t px-4 py-3" style={{ borderColor: "var(--line)" }}>
      <h3 className="mb-1 text-[13px] font-semibold">Spelling</h3>
      <p className="mb-2 text-[12.5px]" style={{ color: "var(--muted)" }}>
        {style === "any"
          ? "Labels spelt more than one way. Set ARGUS_BACKLOG_LABEL_STYLE to hold every label to one spelling as well."
          : `The team spells labels in ${STYLE_NAMES[style]}. Labels spelt otherwise, and labels spelt more than one way.`}
        {style === "any" && split && split.length > 0 && ` Labels of several words in use today: ${split.join(", ")}.`}
      </p>
      {findings.length === 0 ? (
        <p className="py-2 text-[13px]" style={{ color: "var(--faint)" }}>Nothing to tidy.</p>
      ) : (
        <ul className="divide-y" style={{ borderColor: "var(--line)" }}>
          {findings.map((f) => {
            const from = f.variants.map((v) => v.name);
            const keys = (data.rows ?? []).filter((r) => (r.labels ?? []).some((l) => from.includes(l))).map((r) => r.key);
            return (
              <li key={f.target} className="flex flex-wrap items-center gap-2 py-2 text-[13px]">
                <span className="flex flex-wrap items-center gap-1.5">
                  {f.variants.map((v) => (
                    <button key={v.name} onClick={() => onPick(v.name)} className="lnk font-mono" title={`Show the tickets labelled ${v.name}`}>
                      {v.name} <span style={{ color: "var(--muted)" }}>· {v.count}</span>
                    </button>
                  ))}
                </span>
                <span aria-hidden="true" style={{ color: "var(--faint)" }}>→</span>
                <span className="font-mono">{f.target}</span>
                <span className="text-[12px]" style={{ color: "var(--muted)" }}>
                  {f.reason === "spellings" ? "one label, spelt more than one way" : `not ${STYLE_NAMES[style]}`}
                </span>
                <button
                  onClick={() => onBatch({
                    action: "labels.migrate", keys, params: { from, to: f.target }, label: `Migrate ${from.join(", ")} to ${f.target}`,
                  })}
                  disabled={keys.length === 0}
                  className={`${small} ml-auto`}
                  style={outlined}
                >
                  Migrate · {keys.length}
                </button>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}
