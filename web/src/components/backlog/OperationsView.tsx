import type { Backlog, BacklogRow } from "../../api";
import { outlined, small } from "../closeout/Table";
import type { BatchRequest } from "./BatchPanel";
import { Rows, type Selection } from "./Rows";
import { Section } from "./Section";

/* Operations: the requests from the operations team, and what marks
 * them as such.
 *
 * A request is one of three things: labelled with the current label,
 * labelled with a legacy one, or reported by somebody on the roster.
 * The roster is kept behind the gear (OpsSettingsPanel), like the sprint
 * roster. Here, the two fixes each with a one-click batch, then every
 * request together. Only the current label is ever added; the legacy
 * label is replaced or removed, never written, so it dies out. */

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
  const all = ops?.all_keys ?? [];
  // A section's button acts on the rows ticked in that section, or on
  // the whole section when none are, and says which. The bulk bar is
  // for a selection spanning sections; these are the one-click paths.
  const scope = (all: string[]) => {
    const ticked = all.filter((k) => selection.has(k));
    return ticked.length > 0
      ? { all, keys: ticked, count: `${ticked.length} selected` }
      : { all, keys: all, count: `all ${all.length}` };
  };
  const missing = scope(ops?.missing_label_keys ?? []);
  const old = scope(ops?.legacy_label_keys ?? []);
  const work = scope(ops?.work_label_keys ?? []);
  const workLabels = data.settings?.work_labels ?? [];
  const workNames = workLabels.join(", ") || "a work label";

  return (
    <>
      <Section
        title={`Missing the label · ${missing.all.length}`}
        hint={`Counted as a request, by the legacy label or by a reporter on the roster (under the gear), but not labelled ${label}. One click previews adding it to all of them.`}
        right={
          <button
            onClick={() => onBatch({ action: "operations.label", keys: missing.keys, params: {}, label: `Add ${label}` })}
            disabled={missing.keys.length === 0}
            className={`${small} font-medium`}
            style={{ background: "var(--accent)", color: "#fff" }}
          >
            Add {label} · {missing.count}
          </button>
        }
      >
        <Rows rows={pick(missing.all)} selection={selection} staleKeys={staleKeys} empty="Every roster request carries the label." />
      </Section>

      {/* A transitional section: once the old label is gone from every
          open ticket it has nothing to say, and disappears until the
          label creeps back. */}
      {old.all.length > 0 && (
        <Section
          title={`Legacy label · ${old.all.length}`}
          hint={`Labelled ${legacy} rather than ${label}. Replace it where the ticket is a request, or drop it where it was only marking the kind of work; it is read as the same thing until it is gone.`}
          right={
            <span className="flex items-center gap-2">
              <button
                onClick={() => onBatch({ action: "operations.migrate", keys: old.keys, params: {}, label: `Replace ${legacy} with ${label}` })}
                disabled={old.keys.length === 0}
                className={`${small} font-medium`}
                style={{ background: "var(--accent)", color: "#fff" }}
              >
                Replace {legacy} with {label} · {old.count}
              </button>
              <button
                onClick={() => onBatch({
                  action: "labels.remove", keys: old.keys, params: { labels: data.settings?.legacy_labels ?? [] }, label: `Remove ${legacy}`,
                })}
                disabled={old.keys.length === 0 || !(data.settings?.legacy_labels ?? []).length}
                className={small}
                style={outlined}
              >
                Remove {legacy} · {old.count}
              </button>
            </span>
          }
        >
          <Rows rows={pick(old.all)} selection={selection} staleKeys={staleKeys} empty="Nothing carries the legacy label." />
        </Section>
      )}

      {/* Also transitional in spirit: it lists mistakes, and is quiet
          when there are none to show. Judged on the reporter, so an
          engineer's own ticket carrying the same label is not here. */}
      {work.all.length > 0 && (
        <Section
          title={`Carrying a work label · ${work.all.length}`}
          hint={`Reported by someone on the roster but labelled ${workNames}, which marks a kind of engineering work rather than a request. Remove it; the Missing-the-label list above adds ${label} where that is wanted.`}
          right={
            <button
              onClick={() => onBatch({ action: "labels.remove", keys: work.keys, params: { labels: workLabels }, label: `Remove ${workNames}` })}
              disabled={work.keys.length === 0 || workLabels.length === 0}
              className={`${small} font-medium`}
              style={{ background: "var(--accent)", color: "#fff" }}
            >
              Remove {workNames} · {work.count}
            </button>
          }
        >
          <Rows rows={pick(work.all)} selection={selection} staleKeys={staleKeys} empty="No request carries a work label." />
        </Section>
      )}

      <Section
        title={`All requests · ${all.length}`}
        hint="Every open ticket that is an Operations request, by label, legacy label or roster reporter."
      >
        <Rows rows={pick(all)} selection={selection} staleKeys={staleKeys} empty="No open requests." />
      </Section>
    </>
  );
}
