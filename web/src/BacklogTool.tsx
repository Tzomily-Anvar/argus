import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { fetchBacklog, fetchInbox, refreshBacklog } from "./api";
import { navigate, useRoute } from "./route";
import { Icon } from "./components/Icons";
import { SectionTabs, type Section } from "./components/SectionTabs";
import { BatchPanel, type BatchRequest } from "./components/backlog/BatchPanel";
import { BulkBar } from "./components/backlog/BulkBar";
import { GroupsView } from "./components/backlog/GroupsView";
import { InboxView } from "./components/backlog/InboxView";
import { NewView } from "./components/backlog/NewView";
import { OperationsView } from "./components/backlog/OperationsView";
import { jiraBase, type Selection } from "./components/backlog/Rows";
import { BacklogSettingsPanel } from "./components/backlog/BacklogSettingsPanel";
import { ago } from "./components/backlog/Section";

/* The backlog tool.
 *
 * One sweep of the open tickets, read four ways: what is new, which
 * fixed groups they fall in, which are Operations requests, and the
 * personal inbox beside them. The views share one selection, and the
 * selection feeds one bulk bar, so a ticket ticked in Groups can be
 * acted on from Operations without finding it again. Every write goes
 * through the batch panel, previewed first. */

const VIEWS = ["new", "groups", "operations", "inbox"] as const;
type View = (typeof VIEWS)[number];

/* The view is the second segment of the path. New is the bare tool, so
 * /backlog and /backlog/new are the same place. */
const viewOf = (segment: string): View =>
  (VIEWS as readonly string[]).includes(segment) ? (segment as View) : "new";

export function BacklogTool() {
  const qc = useQueryClient();
  const route = useRoute();
  const view = viewOf(route.view);
  const setView = (id: string) => navigate({ tool: "backlog", view: id === "new" ? "" : id });

  const backlog = useQuery({
    queryKey: ["backlog"],
    queryFn: fetchBacklog,
    // Brisk while a sweep runs so the page settles by itself; idle
    // otherwise, so a tab left open all day costs nothing.
    refetchInterval: (q) => (q.state.data?.building ? 2_000 : 60_000),
  });
  const inbox = useQuery({ queryKey: ["inbox"], queryFn: fetchInbox, refetchInterval: 60_000 });
  const refresh = useMutation({
    mutationFn: refreshBacklog,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["backlog"] });
      qc.invalidateQueries({ queryKey: ["inbox"] });
    },
  });

  // The selection: one set for the whole tool. Kept as a Set for the
  // lookups the table does per row, and handed down as the small
  // interface the views need rather than the setter itself.
  const [selected, setSelected] = useState<Set<string>>(() => new Set());
  const selection: Selection = useMemo(() => ({
    keys: [...selected],
    has: (key) => selected.has(key),
    toggle: (key) => setSelected((s) => {
      const next = new Set(s);
      if (next.has(key)) next.delete(key); else next.add(key);
      return next;
    }),
    set: (keys, on) => setSelected((s) => {
      const next = new Set(s);
      for (const k of keys) { if (on) next.add(k); else next.delete(k); }
      return next;
    }),
    clear: () => setSelected(new Set()),
  }), [selected]);

  const [batch, setBatch] = useState<BatchRequest | null>(null);
  const [settings, setSettings] = useState(false);

  const data = backlog.data;
  const rows = data?.rows ?? [];
  const base = jiraBase(rows);
  const staleKeys = useMemo(
    () => new Set((data?.groups ?? []).find((g) => g.id === "stale")?.keys ?? []),
    [data],
  );
  const fresh = rows.filter((r) => r.new && !r.acknowledged).length;
  const grouped = (data?.groups ?? []).reduce((n, g) => n + (g.keys ?? []).length, 0);
  const inboxLive = (inbox.data?.items ?? []).filter((i) => !i.dismissed).length;

  const sections: Section[] = [
    { id: "new", label: "New", count: fresh, urgent: true },
    { id: "groups", label: "Groups", count: grouped },
    { id: "operations", label: "Operations", count: (data?.operations?.all_keys ?? []).length },
    { id: "inbox", label: "Inbox", count: inboxLive },
  ];

  const warnings = data?.warnings ?? [];

  return (
    <div className="mx-auto max-w-5xl px-6 pt-6 pb-20">
      <header className="mb-5 flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-lg font-semibold tracking-tight">Backlog</h1>
          {data && (
            <p className="mt-0.5 text-[13px]" style={{ color: "var(--muted)" }}>
              {rows.length} open {rows.length === 1 ? "ticket" : "tickets"} in the sweep
            </p>
          )}
        </div>
        <div className="flex items-center gap-2">
          <span className="text-xs" style={{ color: "var(--faint)" }}>
            {refresh.isPending || data?.building ? "sweeping…" : data ? `as of ${ago(data.swept_at)}` : ""}
          </span>
          {/* The operations roster: about the team rather than the
              sweep, so it lives behind the gear as the sprint roster does. */}
          <button
            onClick={() => setSettings(true)}
            aria-label="Settings"
            title="The operations roster, and the labels the bulk bar offers"
            className="flex items-center rounded-lg px-2.5 py-2 text-sm font-medium"
            style={{ background: "var(--surface)", border: "1px solid var(--line)", boxShadow: "var(--shadow)" }}
          >
            <Icon.gear />
          </button>
          <button
            onClick={() => refresh.mutate()}
            disabled={refresh.isPending || data?.building}
            className="flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-sm font-medium disabled:opacity-50"
            style={{ background: "var(--surface)", border: "1px solid var(--line)", boxShadow: "var(--shadow)" }}
          >
            <Icon.refresh />
            Refresh
          </button>
        </div>
      </header>

      {backlog.error && (
        <p className="mb-4 rounded-xl px-3.5 py-2.5 text-sm" style={{ background: "var(--crit-bg)", color: "var(--crit)" }}>
          {backlog.error instanceof Error ? backlog.error.message : String(backlog.error)}
        </p>
      )}
      {refresh.error && (
        <p className="mb-4 rounded-xl px-3.5 py-2.5 text-sm" style={{ background: "var(--crit-bg)", color: "var(--crit)" }}>
          {refresh.error instanceof Error ? refresh.error.message : String(refresh.error)}
        </p>
      )}

      {/* What the sweep could not do, said above the figures it affects:
          a count built from a partial read is a count to doubt. */}
      {warnings.length > 0 && (
        <p className="mb-4 rounded-xl px-3.5 py-2.5 text-sm" style={{ background: "var(--warn-bg)", color: "var(--warn)" }}>
          {warnings.join(" · ")}
        </p>
      )}

      {backlog.isLoading && (
        <p className="text-sm" style={{ color: "var(--muted)" }}>
          Running the first sweep. This takes a few seconds; afterwards the backlog is served from memory.
        </p>
      )}

      {data && (
        <>
          <SectionTabs sections={sections} active={view} onSelect={setView} />

          {view === "new" && <NewView rows={rows} selection={selection} staleKeys={staleKeys} />}
          {view === "groups" && (
            <GroupsView
              groups={data.groups ?? []}
              epics={data.epics ?? []}
              rows={rows}
              base={base}
              selection={selection}
              staleKeys={staleKeys}
            />
          )}
          {view === "operations" && (
            <OperationsView data={data} selection={selection} staleKeys={staleKeys} onBatch={setBatch} />
          )}
          {view === "inbox" && <InboxView inbox={inbox.data} loading={inbox.isLoading} error={inbox.error} />}

          <BulkBar selection={selection} data={data} base={base} onBatch={setBatch} />
        </>
      )}

      {settings && <BacklogSettingsPanel onClose={() => setSettings(false)} />}

      {batch && (
        <BatchPanel
          request={batch}
          onClose={() => setBatch(null)}
          onApplied={() => {
            // The server queues a sweep after a write. Ask once now and
            // once after it has had a moment to start, so the page sees
            // "sweeping" and keeps polling until the new picture lands.
            qc.invalidateQueries({ queryKey: ["backlog"] });
            setTimeout(() => qc.invalidateQueries({ queryKey: ["backlog"] }), 2_000);
            selection.clear();
          }}
        />
      )}
    </div>
  );
}
