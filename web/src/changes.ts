import { useState } from "react";
import type { ChangeOp, ChangeRequest, StoredPerson } from "./api";

/* The local draft of what a person wants written back.
 *
 * Nothing here is sent anywhere. Typing a value into a flag, or queueing
 * an entry under Carryover, adds a request to this list; Preview posts
 * the list and the server answers with what it would and would not do.
 * Holding the draft in the browser rather than on the server keeps the
 * one rule that matters legible: until Preview is pressed, Argus has not
 * been told anything. */

export type Queued = { id: string; request: ChangeRequest };

export type ChangeDraft = {
  list: Queued[];
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

/** useChangeDraft holds the requests queued against one sprint. Kept per
 *  sprint so that, where the view survives a change of sprint, the work
 *  typed against the other one is still there on the way back. */
export function useChangeDraft(sprintNumber: number): ChangeDraft {
  const [drafts, setDrafts] = useState<Record<number, Queued[]>>({});
  const list = drafts[sprintNumber] ?? [];

  const update = (fn: (prev: Queued[]) => Queued[]) =>
    setDrafts((prev) => {
      const next = fn(prev[sprintNumber] ?? []);
      const all = { ...prev };
      if (next.length === 0) delete all[sprintNumber];
      else all[sprintNumber] = next;
      return all;
    });

  return {
    list,
    add: (request) => update((prev) => [...prev, { id: nextID(), request }]),
    remove: (id) => update((prev) => prev.filter((q) => q.id !== id)),
    clear: () => update(() => []),
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
