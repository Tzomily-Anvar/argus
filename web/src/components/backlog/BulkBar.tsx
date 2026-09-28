import { useState } from "react";
import type { Backlog } from "../../api";
import { Badge } from "../Badge";
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
  const [labelling, setLabelling] = useState(false);
  const [labels, setLabels] = useState("");
  const keys = selection.keys;
  const n = keys.length;
  if (n === 0) return null;

  const ops = data.settings?.operations_label || "Operations";
  const ask = (action: BatchRequest["action"], params: Record<string, unknown>, label: string) =>
    onBatch({ action, keys, params, label });

  const sprints = (data.sprints ?? []).map((s) => ({ id: String(s.id), label: s.name, hint: s.state }));
  const epics = (data.epics_all ?? []).map((e) => ({ id: e.key, label: `${e.key} · ${e.summary}` }));
  const stories = (data.stories ?? []).map((s) => ({ id: s.key, label: `${s.key} · ${s.summary}`, hint: s.epic_key || undefined }));

  const addLabels = () => {
    const list = labels.split(/[\s,]+/).map((l) => l.trim()).filter(Boolean);
    if (list.length === 0) return;
    ask("labels.add", { labels: list }, `Add ${list.length === 1 ? "label" : "labels"} ${list.join(", ")}`);
    setLabelling(false);
    setLabels("");
  };

  return (
    <div
      className="card sticky bottom-4 z-30 mt-4 flex flex-wrap items-center gap-2 px-4 py-2.5"
      style={{ background: "var(--surface)" }}
    >
      <Badge tone="info" label={`${n} selected`} />
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

      {labelling ? (
        <span className="flex items-center gap-1">
          <input
            autoFocus
            value={labels}
            onChange={(e) => setLabels(e.target.value)}
            onKeyDown={(e) => { if (e.key === "Enter") addLabels(); if (e.key === "Escape") setLabelling(false); }}
            placeholder="label, another"
            aria-label="Labels to add"
            className="w-40 rounded-lg px-2.5 py-1.5 text-[13px] outline-none"
            style={{ background: "var(--surface-2)", border: "1px solid var(--line)", color: "var(--ink)" }}
          />
          <button onClick={addLabels} disabled={!labels.trim()} className={small} style={outlined}>Add</button>
          <button onClick={() => setLabelling(false)} className={small} style={outlined}>Cancel</button>
        </span>
      ) : (
        <button
          onClick={() => setLabelling(true)}
          className="rounded-lg px-3 py-1.5 text-[13px] font-medium"
          style={{ background: "var(--surface)", border: "1px solid var(--line)", color: "var(--ink)" }}
        >
          Add label…
        </button>
      )}
      <button
        onClick={() => ask("operations.label", {}, `Add ${ops}`)}
        className="rounded-lg px-3 py-1.5 text-[13px] font-medium"
        style={{ background: "var(--surface)", border: "1px solid var(--line)", color: "var(--ink)" }}
      >
        Add {ops}
      </button>
      {/* Named in the alert colour with its word, and never the default:
          it is the one action here that cannot be reversed. */}
      <button
        onClick={() => ask("issue.delete", {}, `Delete ${n} ${n === 1 ? "ticket" : "tickets"}`)}
        className="rounded-lg px-3 py-1.5 text-[13px] font-medium"
        style={{ background: "var(--surface)", border: "1px solid var(--crit)", color: "var(--crit)" }}
      >
        Delete…
      </button>

      <button onClick={selection.clear} className={`${small} ml-auto`} style={outlined}>Clear</button>
    </div>
  );
}
