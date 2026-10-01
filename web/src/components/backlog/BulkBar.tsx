import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { fetchLabels, type Backlog } from "../../api";
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
  // One input serves the label actions; which verb it was opened with
  // decides what the preview asks for. A migration is two picks, the
  // label to replace and then the one to put in its place; the first is
  // held here between them.
  const [labelling, setLabelling] = useState<"add" | "remove" | "migrate" | null>(null);
  const [labels, setLabels] = useState("");
  const [migrating, setMigrating] = useState<string | null>(null);
  // The labels chosen behind the gear. With any chosen, the two label
  // buttons are pickers over them with "Other…" at the end for a typed
  // one; with none, they open the input straight away. A migration picks
  // from the chosen ones and every label in use, since what it replaces
  // is by definition a label the team would not choose to offer.
  const chosen = useQuery({ queryKey: ["labels"], queryFn: fetchLabels, staleTime: 60_000 });
  const shelf = (chosen.data?.labels ?? []).map((l) => l.name);
  const inUse = (chosen.data?.in_use ?? []).map((l) => l.name);
  const known = [...new Set([...shelf, ...inUse])];
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

  const sendLabelList = (verb: "add" | "remove" | "migrate", list: string[]) => {
    if (list.length === 0) return;
    if (verb === "migrate") {
      if (migrating) ask("labels.migrate", { from: [migrating], to: list[0] }, `Migrate ${migrating} to ${list[0]}`);
    } else {
      ask(verb === "add" ? "labels.add" : "labels.remove", { labels: list },
        `${verb === "add" ? "Add" : "Remove"} ${list.length === 1 ? "label" : "labels"} ${list.join(", ")}`);
    }
    setLabelling(null);
    setMigrating(null);
    setLabels("");
  };
  const sendLabels = () => {
    if (!labelling) return;
    sendLabelList(labelling, labels.split(/[\s,]+/).map((l) => l.trim()).filter(Boolean));
  };
  const OTHER = "\u0000other";
  const labelPicker = (verb: "add" | "remove") => (
    <Picker
      label={verb === "add" ? "Add label" : "Remove label"}
      items={[...shelf.map((l) => ({ id: l, label: l })), { id: OTHER, label: "Other…", hint: "type one" }]}
      up
      placeholder="Find a label…"
      onPick={(id) => (id === OTHER ? setLabelling(verb) : sendLabelList(verb, [id]))}
    />
  );
  // The second pick of a migration: anything known but the label being
  // replaced, or a typed one.
  const migratePicker = migrating ? (
    <span className="flex items-center gap-1">
      <Picker
        label={`Migrate ${migrating} to`}
        items={[...known.filter((l) => l !== migrating).map((l) => ({ id: l, label: l })), { id: OTHER, label: "Other…", hint: "type one" }]}
        up
        placeholder="Find the new label…"
        onPick={(id) => (id === OTHER ? setLabelling("migrate") : sendLabelList("migrate", [id]))}
      />
      <button onClick={() => setMigrating(null)} className={small} style={outlined}>Cancel</button>
    </span>
  ) : (
    <Picker
      label="Migrate label"
      items={known.map((l) => ({ id: l, label: l }))}
      up
      placeholder="Find the label to replace…"
      onPick={setMigrating}
      disabled={known.length === 0}
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

      {labelling ? (
        <span className="flex items-center gap-1">
          <input
            autoFocus
            value={labels}
            onChange={(e) => setLabels(e.target.value)}
            onKeyDown={(e) => { if (e.key === "Enter") sendLabels(); if (e.key === "Escape") setLabelling(null); }}
            placeholder={labelling === "migrate" ? "new label" : "label, another"}
            aria-label={labelling === "add" ? "Labels to add" : labelling === "remove" ? "Labels to remove" : "The label to migrate to"}
            className="w-40 rounded-lg px-2.5 py-1.5 text-[13px] outline-none"
            style={{ background: "var(--surface-2)", border: "1px solid var(--line)", color: "var(--ink)" }}
          />
          <button onClick={sendLabels} disabled={!labels.trim()} className={small} style={outlined}>
            {labelling === "add" ? "Add" : labelling === "remove" ? "Remove" : `Migrate ${migrating ?? ""}`}
          </button>
          <button onClick={() => { setLabelling(null); setMigrating(null); }} className={small} style={outlined}>Cancel</button>
        </span>
      ) : migrating ? (
        migratePicker
      ) : shelf.length > 0 ? (
        <>
          {labelPicker("add")}
          {labelPicker("remove")}
          {migratePicker}
        </>
      ) : (
        <>
          <button
            onClick={() => setLabelling("add")}
            className="rounded-lg px-3 py-1.5 text-[13px] font-medium"
            style={{ background: "var(--surface)", border: "1px solid var(--line)", color: "var(--ink)" }}
          >
            Add label…
          </button>
          <button
            onClick={() => setLabelling("remove")}
            className="rounded-lg px-3 py-1.5 text-[13px] font-medium"
            style={{ background: "var(--surface)", border: "1px solid var(--line)", color: "var(--ink)" }}
          >
            Remove label…
          </button>
          {migratePicker}
        </>
      )}
      <button
        onClick={() => ask("request.label", {}, `Add ${requestLabel}`)}
        className="rounded-lg px-3 py-1.5 text-[13px] font-medium"
        style={{ background: "var(--surface)", border: "1px solid var(--line)", color: "var(--ink)" }}
      >
        Add {requestLabel}
      </button>
      {/* Named in the alert colour with its word, and never the default:
          it is the one action here that cannot be reversed. Behind its
          own switch, and shown greyed with the reason rather than hidden,
          so a deployment with it off still says the tool can do it. */}
      <button
        onClick={() => ask("issue.delete", {}, `Delete ${n} ${n === 1 ? "ticket" : "tickets"}`)}
        disabled={!data.settings?.allow_delete}
        title={data.settings?.allow_delete
          ? "Delete the selected tickets from Jira, after a preview and a typed count"
          : "Deleting is off for this deployment: ARGUS_BACKLOG_ALLOW_DELETE is unset"}
        className="rounded-lg px-3 py-1.5 text-[13px] font-medium disabled:opacity-40"
        style={{ background: "var(--surface)", border: "1px solid var(--crit)", color: "var(--crit)" }}
      >
        Delete…{data.settings?.allow_delete ? "" : " (off)"}
      </button>

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
