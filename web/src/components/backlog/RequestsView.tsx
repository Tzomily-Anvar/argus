import type { Backlog, BacklogRow } from "../../api";
import { outlined, small } from "../closeout/Table";
import type { BatchRequest } from "./BatchPanel";
import { Rows, type Selection } from "./Rows";
import { Section, scopeOf } from "./Section";

/* Requests: the tickets that come from outside the team, and what marks
 * them as such.
 *
 * A typical use is an operations or support team whose tickets
 * engineering triages, but the tool does not assume that: a request is
 * one of three things, labelled with the configured request label,
 * labelled with a legacy spelling of it, or reported by one of the
 * requesters kept behind the gear (BacklogSettingsPanel). Here, the
 * fixes each with a one-click batch, then every request together. Only
 * the current label is ever added; a legacy label is replaced or
 * removed, never written, so it dies out. */

export function RequestsView({
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
  const reqs = data.requests;
  const label = data.settings?.request_label || "Request";
  const legacyLabels = data.settings?.legacy_labels ?? [];
  const legacy = legacyLabels.join(", ") || "the legacy label";
  const all = reqs?.all_keys ?? [];
  const missing = scopeOf(reqs?.missing_label_keys ?? [], selection);
  const old = scopeOf(reqs?.legacy_label_keys ?? [], selection);
  const work = scopeOf(reqs?.work_label_keys ?? [], selection);
  const workLabels = data.settings?.work_labels ?? [];
  const workNames = workLabels.join(", ") || "a work label";

  return (
    <>
      <Section
        title={`Missing the label · ${missing.all.length}`}
        hint={`Counted as a request, by a legacy label or by a requester as reporter (under the gear), but not labelled ${label}. One click previews adding it to all of them.`}
        right={
          <button
            onClick={() => onBatch({ action: "request.label", keys: missing.keys, params: {}, label: `Add ${label}` })}
            disabled={missing.keys.length === 0}
            className={`${small} font-medium`}
            style={{ background: "var(--accent)", color: "#fff" }}
          >
            Add {label} · {missing.count}
          </button>
        }
      >
        <Rows rows={pick(missing.all)} selection={selection} staleKeys={staleKeys} empty="Every request carries the label." />
      </Section>

      {/* A transitional section: once the old label is gone from every
          open ticket it has nothing to say, and disappears until the
          label creeps back. The replace is the generic migration with
          the legacy labels as what goes and the request label as what
          comes. */}
      {old.all.length > 0 && (
        <Section
          title={`Legacy label · ${old.all.length}`}
          hint={`Labelled ${legacy} rather than ${label}. Replace it where the ticket is a request, or drop it where it was only marking the kind of work; it is read as the same thing until it is gone.`}
          right={
            <span className="flex items-center gap-2">
              <button
                onClick={() => onBatch({
                  action: "labels.migrate", keys: old.keys, params: { from: legacyLabels, to: label }, label: `Replace ${legacy} with ${label}`,
                })}
                disabled={old.keys.length === 0 || legacyLabels.length === 0}
                className={`${small} font-medium`}
                style={{ background: "var(--accent)", color: "#fff" }}
              >
                Replace {legacy} with {label} · {old.count}
              </button>
              <button
                onClick={() => onBatch({
                  action: "labels.remove", keys: old.keys, params: { labels: legacyLabels }, label: `Remove ${legacy}`,
                })}
                disabled={old.keys.length === 0 || legacyLabels.length === 0}
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
          hint={`Reported by a requester but labelled ${workNames}, which marks a kind of engineering work rather than a request. Remove it; the Missing-the-label list above adds ${label} where that is wanted.`}
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
        hint={`Every open ticket that is a request from outside the team: labelled ${label}, labelled with a legacy spelling, or reported by a requester.`}
      >
        <Rows rows={pick(all)} selection={selection} staleKeys={staleKeys} empty="No open requests." />
      </Section>
    </>
  );
}
