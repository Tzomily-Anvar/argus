import { useSyncExternalStore } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { deleteDraft, fetchDraft, saveDraft, type ChangeOp, type ChangeRequest, type StoredPerson } from "./api";

/* The draft of what a person wants written back.
 *
 * Typing a value into a flag, queueing an entry under Carryover, or
 * accepting a row in the close-out adds a request to this list; Preview
 * posts the list and the server answers with what it would and would
 * not do. The list itself lives in the store, one Draft per sprint, so a
 * close can be abandoned at step three and picked up after a reload, a
 * restart, or on another machine. Nothing in it reaches Jira: the draft
 * is a note to self that the server keeps, and Preview is still the
 * first moment Argus is asked to do anything with it. */

export type Queued = { id: string; request: ChangeRequest };

export type DraftSaving = "idle" | "pending" | "saved" | "error";

export type ChangeDraft = {
  list: Queued[];
  /** True until the stored draft has been read once. The inputs still
   *  work meanwhile; a value typed before the answer arrives wins. */
  loading: boolean;
  /** Whether the last write to the store has landed. */
  saving: DraftSaving;
  add: (request: ChangeRequest) => void;
  remove: (id: string) => void;
  clear: () => void;
  /** One request per key and operation, for the inputs that edit a
   *  single value: setting it again replaces, setting null removes. */
  set: (key: string, op: ChangeOp, request: ChangeRequest | null) => void;
  find: (key: string, op: ChangeOp) => Queued | undefined;
  forKey: (key: string) => Queued[];
};

let counter = 0;
const nextID = () => `q${++counter}`;

/* One saver per sprint, outside React.
 *
 * Two places show the same draft at once - the report's inline inputs
 * and the close-out panel - and both write to it. The list is shared
 * through the query cache; the debounce timer and the "saving…" state
 * have to be shared too, or closing the panel 300ms after a keystroke
 * would drop the write with the component that scheduled it. */
type Saver = { timer?: ReturnType<typeof setTimeout>; state: DraftSaving; listeners: Set<() => void> };
const savers = new Map<number, Saver>();

function saverFor(id: number): Saver {
  let s = savers.get(id);
  if (!s) {
    s = { state: "idle", listeners: new Set() };
    savers.set(id, s);
  }
  return s;
}

function setState(s: Saver, state: DraftSaving) {
  s.state = state;
  s.listeners.forEach((fn) => fn());
}

/** Long enough that a number typed digit by digit is one write, short
 *  enough that closing the tab a moment later has already saved. */
const DEBOUNCE_MS = 600;

function schedule(id: number, list: Queued[]) {
  const s = saverFor(id);
  if (s.timer) clearTimeout(s.timer);
  setState(s, "pending");
  s.timer = setTimeout(async () => {
    s.timer = undefined;
    try {
      if (list.length === 0) await deleteDraft(id);
      else await saveDraft({ sprint_jira_id: id, requests: list.map((q) => q.request) });
      setState(s, "saved");
    } catch {
      setState(s, "error");
    }
  }, DEBOUNCE_MS);
}

function discard(id: number) {
  const s = saverFor(id);
  if (s.timer) clearTimeout(s.timer);
  s.timer = undefined;
  setState(s, "pending");
  deleteDraft(id)
    .then(() => setState(s, "saved"))
    .catch(() => setState(s, "error"));
}

/** useChangeDraft holds the requests queued against one sprint, read
 *  from the store on first use and written back as they change. Callers
 *  pass both identifiers because they have both: the report is addressed
 *  by sprint number, the store by Jira id, and only the second is used
 *  here. */
export function useChangeDraft(_sprintNumber: number, sprintJiraID: number): ChangeDraft {
  const qc = useQueryClient();
  const queryKey = ["draft", sprintJiraID];

  // The ids are minted on read: the store keeps requests, not ids, and
  // an id only has to be stable for as long as the list is on screen.
  // Never refetched on its own account, because a refetch would replace
  // the list under somebody typing into it; the apply invalidates it
  // when the server has cleared the applied rows.
  const stored = useQuery({
    queryKey,
    queryFn: async (): Promise<Queued[]> => {
      const d = await fetchDraft(sprintJiraID);
      return (d.requests ?? []).map((request) => ({ id: nextID(), request }));
    },
    staleTime: Infinity,
    refetchOnWindowFocus: false,
  });
  const list = stored.data ?? [];

  const saver = saverFor(sprintJiraID);
  const saving = useSyncExternalStore(
    (fn) => {
      saver.listeners.add(fn);
      return () => saver.listeners.delete(fn);
    },
    () => saver.state,
  );

  const update = (fn: (prev: Queued[]) => Queued[]) => {
    const next = fn(qc.getQueryData<Queued[]>(queryKey) ?? []);
    qc.setQueryData<Queued[]>(queryKey, next);
    schedule(sprintJiraID, next);
  };

  return {
    list,
    loading: stored.isLoading,
    saving,
    add: (request) => update((prev) => [...prev, { id: nextID(), request }]),
    remove: (id) => update((prev) => prev.filter((q) => q.id !== id)),
    // Discard is immediate rather than debounced: it is the one edit
    // where "did that take" matters at once.
    clear: () => {
      qc.setQueryData<Queued[]>(queryKey, []);
      discard(sprintJiraID);
    },
    set: (key, op, request) =>
      update((prev) => {
        const rest = prev.filter((q) => !(q.request.key === key && q.request.op === op));
        return request ? [...rest, { id: nextID(), request }] : rest;
      }),
    find: (key, op) => list.find((q) => q.request.key === key && q.request.op === op),
    forKey: (key) => list.filter((q) => q.request.key === key),
  };
}

/** figure prints a number the way a person would write it: 3, not 3.00,
 *  and 2.5 rather than 2.50. The same rule as the server's. */
export function figure(n: number): string {
  return String(Math.round(n * 100) / 100);
}

/** hoursLabel is the hours with the points they convert to beside them:
 *  "6h · 1 pts". The conversion is shown rather than implied, because a
 *  person recalling "about a day" types the hours and should see what
 *  the report will make of them. */
export function hoursLabel(hours: number, hoursPerPoint?: number): string {
  const h = `${figure(hours)}h`;
  if (!hoursPerPoint || hoursPerPoint <= 0) return h;
  return `${h} · ${figure(hours / hoursPerPoint)} pts`;
}

export function nameOf(roster: StoredPerson[], accountID?: string): string {
  if (!accountID) return "";
  return roster.find((p) => p.account_id === accountID)?.name ?? accountID;
}

/** opLabel is the operation as a person would say it. */
export function opLabel(op: ChangeOp): string {
  switch (op) {
    case "points.set":
      return "Story points";
    case "assignee.set":
      return "Assignee";
    case "worklog.add":
      return "Worklog · add";
    case "worklog.update":
      return "Worklog · change";
    case "worklog.delete":
      return "Worklog · remove";
  }
}

/** describe is one queued request in a line, for showing what is queued
 *  beside the place it was typed. */
export function describe(req: ChangeRequest, roster: StoredPerson[], hoursPerPoint?: number): string {
  switch (req.op) {
    case "points.set":
      return `Story points → ${figure(req.points ?? 0)}`;
    case "assignee.set":
      return `Assignee → ${nameOf(roster, req.assignee)}`;
    case "worklog.add":
      return `Add ${hoursLabel(req.hours ?? 0, hoursPerPoint)} for ${nameOf(roster, req.person)}`;
    case "worklog.update":
      return `Change entry ${req.worklog_id} to ${hoursLabel(req.hours ?? 0, hoursPerPoint)}`;
    case "worklog.delete":
      return `Remove entry ${req.worklog_id}`;
  }
}
