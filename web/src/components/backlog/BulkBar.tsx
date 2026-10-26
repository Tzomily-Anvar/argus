import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { fetchLabels, type Backlog } from "../../api";
import { labelItems, newLabelItems } from "../../labels";
import { Badge } from "../Badge";
import { Button } from "../Button";
import { outlined, small } from "../closeout/Table";
import type { BatchRequest } from "./BatchPanel";
import { Picker } from "./Picker";
import { jqlURL, keysJQL, type Selection } from "./Rows";

/* The bulk bar: what can be done to the selection.
 *
 * Sticky at the foot of the page so the count is in view wherever the
 * last box was ticked, and only there while something is selected. Each
 * button opens the batch panel with a preview; nothing here writes, and
 * nothing leaves the browser until the panel asks the server what it
 * would do. The pickers are searchable because a board has more sprints,
 * epics and stories than a select can be scrolled through. */

export function BulkBar({
  selection, data, base, onBatch,
}: {
  selection: Selection;
  data: Backlog;
  base: string;
  onBatch: (req: BatchRequest) => void;
}) {
  // A migration is two picks, the label to replace and then the one to
  // put in its place; the first is held here between them.
  const [migrating, setMigrating] = useState<string | null>(null);
  // Adding offers every label there is, in groups: the ones chosen
  // behind the gear first, then the active repositories, the rest in
  // use and the rest of the site; and a label typed that is none of
  // them is created, in the team's spelling, unless it is a spelling of
  // one that exists. Removing offers what the selection carries.
  const labels = useQuery({ queryKey: ["labels"], queryFn: fetchLabels, staleTime: 60_000 });
  const style = labels.data?.style ?? "any";
  const offered = labelItems(labels.data);
  const known = offered.map((i) => i.id);
  // While migrating, the label being replaced is not a label to offer
  // instead of a new one: respelling it is the point.
  const create = (q: string) => newLabelItems(q, known.filter((l) => l !== migrating), style);
  // The selection is one set across every view and section, so the
  // count can be larger than the ticks on screen. Opening the badge
  // lists every key, each with its own untick.
  const [listing, setListing] = useState(false);
  const keys = selection.keys;
  const n = keys.length;
  if (n === 0) return null;

  const requestLabel = data.settings?.request_label || "Request";
  const ask = (action: BatchRequest["action"], params: Record<string, unknown>, label: string) =>
    onBatch({ action, keys, params, label });

  const sprints = (data.sprints ?? []).map((s) => ({ id: String(s.id), label: s.name, hint: s.state }));
  const epics = (data.epics_all ?? []).map((e) => ({ id: e.key, label: `${e.key} · ${e.summary}` }));
  const stories = (data.stories ?? []).map((s) => ({ id: s.key, label: `${s.key} · ${s.summary}`, hint: s.epic_key || undefined }));

  // The labels on the selected tickets, most carried first, each saying
  // on how many of them it is.
  const selected = new Set(keys);
  const carried = new Map<string, number>();
  for (const r of data.rows ?? []) {
    if (!selected.has(r.key)) continue;
    for (const l of r.labels ?? []) carried.set(l, (carried.get(l) ?? 0) + 1);
  }
  const removable = [...carried]
    .sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))
    .map(([l, c]) => ({ id: l, label: l, hint: `on ${c} of ${n}` }));

  // The second pick of a migration: anything offered but the label being
  // replaced, or a new one.
  const migratePicker = migrating ? (
    <span className="flex items-center gap-1">
      <Picker
        label={`Migrate ${migrating} to`}
        items={labelItems(labels.data, [migrating])}
        extra={create}
        up
        placeholder="Find or type the new label…"
        onPick={(to) => {
          ask("labels.migrate", { from: [migrating], to }, `Migrate ${migrating} to ${to}`);
          setMigrating(null);
        }}
      />
      <button onClick={() => setMigrating(null)} className={small} style={outlined}>Cancel</button>
    </span>
  ) : (
    <Picker
      label="Migrate label"
      items={removable}
      up
      placeholder="Find the label to replace…"
      onPick={setMigrating}
      disabled={removable.length === 0}
    />
  );

  return (
    <div
      className="card sticky bottom-4 z-30 mt-4 flex flex-wrap items-center gap-2 px-4 py-2.5"
      style={{ background: "var(--surface)" }}
    >
      <button
        onClick={() => setListing((v) => !v)}
        className="rounded-md"
        aria-expanded={listing}
        title={listing ? "Hide the selection" : "Show which tickets are selected, across every view"}
      >
        <Badge tone="info" label={`${n} selected${listing ? " ▴" : " ▾"}`} />
      </button>
      {base && (
        <a href={jqlURL(base, keysJQL(keys))} target="_blank" rel="noreferrer" className="lnk text-xs" title={keysJQL(keys)}>
          Open in Jira →
        </a>
      )}
      <span aria-hidden="true" style={{ color: "var(--line)" }}>·</span>

      <Picker
        label="Assign to sprint"
        items={sprints}
        up
        placeholder="Find a sprint…"
        onPick={(id) => {
          const s = (data.sprints ?? []).find((x) => String(x.id) === id);
          ask("sprint.assign", { sprint_id: Number(id) }, `Assign to ${s?.name ?? "sprint"}`);
        }}
      />
      <Picker
        label="Set epic"
        items={epics}
        up
        placeholder="Find an epic…"
        onPick={(key) => ask("epic.set", { epic_key: key }, `Set epic to ${key}`)}
      />
      <Picker
        label="Link to story"
        items={stories}
        up
        placeholder="Find a story…"
        onPick={(key) => ask("story.link", { story_key: key }, `Link to story ${key}`)}
      />

      {migrating ? (
        migratePicker
      ) : (
        <>
          <Picker
            label="Add label"
            items={offered}
            extra={create}
            up
            placeholder="Find or type a label…"
            onPick={(l) => ask("labels.add", { labels: [l] }, `Add label ${l}`)}
            disabled={labels.isLoading}
          />
          <Picker
            label="Remove label"
            items={removable}
            up
            placeholder="Find a label…"
            onPick={(l) => ask("labels.remove", { labels: [l] }, `Remove label ${l}`)}
            disabled={removable.length === 0}
          />
          {migratePicker}
        </>
      )}
      <Button onClick={() => ask("request.label", {}, `Add ${requestLabel}`)}>Add {requestLabel}</Button>
      {/* Named in the alert colour with its word, and never the default:
          it is the one action here that cannot be reversed. Behind its
          own switch, and shown greyed with the reason rather than hidden,
          so a deployment with it off still says the tool can do it. */}
      <Button
        variant="danger"
        onClick={() => ask("issue.delete", {}, `Delete ${n} ${n === 1 ? "ticket" : "tickets"}`)}
        disabled={!data.settings?.allow_delete}
        title={data.settings?.allow_delete
          ? "Delete the selected tickets from Jira, after a preview and a typed count"
          : "Deleting is off for this deployment: ARGUS_BACKLOG_ALLOW_DELETE is unset"}
      >
        Delete…{data.settings?.allow_delete ? "" : " (off)"}
      </Button>

      <button onClick={selection.clear} className={`${small} ml-auto`} style={outlined}>Clear</button>

      {listing && (
        <div className="flex w-full flex-wrap gap-1.5 border-t pt-2" style={{ borderColor: "var(--line)" }}>
          {keys.map((k) => (
            <span
              key={k}
              className="inline-flex items-center gap-1 rounded-md px-1.5 py-0.5 font-mono text-[11.5px]"
              style={{ background: "var(--surface-2)", color: "var(--ink)" }}
            >
              {k}
              <button onClick={() => selection.toggle(k)} aria-label={`Unselect ${k}`} title="Unselect" style={{ color: "var(--faint)" }}>×</button>
            </span>
          ))}
        </div>
      )}
    </div>
  );
}
