import { useState } from "react";
import type { SprintFlag, StoredPerson } from "../api";
import type { ChangeDraft } from "../changes";
import { Badge } from "./Badge";
import { parse } from "./Panel";

/* The two inputs a flag can grow.
 *
 * "Needs a look" already lists exactly the tickets a sprint close has to
 * fix: finished with no points, finished with nobody assigned. Rather
 * than a separate form, the fix is typed on the flag itself, and a typed
 * value is a queued request, cleared by clearing the field. Nothing is
 * sent until Preview.
 *
 * Every other kind of flag is left alone: they name things a person has
 * to go and look at, not values Argus could write. */

/** A person from the roster, or nobody. Only people opted in are offered:
 *  work is not assigned to, or logged for, somebody who has left. */
export function PersonSelect({
  roster, value, onChange, label, disabled,
}: {
  roster: StoredPerson[];
  value: string;
  onChange: (accountID: string) => void;
  label: string;
  disabled?: boolean;
}) {
  const chosen = value !== "";
  return (
    <select
      aria-label={label}
      value={value}
      disabled={disabled}
      onChange={(e) => onChange(e.target.value)}
      className="rounded px-2 py-1 text-[13px] outline-none disabled:opacity-50"
      style={{
        background: "var(--surface-2)",
        border: `1px solid ${chosen ? "var(--accent)" : "var(--line)"}`,
        color: chosen ? "var(--ink)" : "var(--muted)",
      }}
    >
      <option value="">— choose a person —</option>
      {roster.filter((p) => p.active).map((p) => (
        <option key={p.account_id} value={p.account_id}>{p.name}</option>
      ))}
    </select>
  );
}

/** A number where there is no stored value to start from.
 *
 *  Not the shared NumberField, deliberately: that one shows a stored
 *  value until you type over it, and here there is none - a ticket with
 *  no points, an entry not yet logged. Showing "0" would put a number on
 *  the screen that nobody typed, which is the one thing a change set
 *  must never contain. The styling is NumberField's, so it reads as the
 *  same kind of control. */
export function BlankNumberField({
  value, onChange, label, placeholder, width = "w-16",
}: {
  value: string;
  onChange: (s: string) => void;
  label: string;
  placeholder: string;
  width?: string;
}) {
  const edited = value !== "";
  const bad = edited && Number.isNaN(parse(value));
  return (
    <input
      type="number"
      step="0.5"
      min="0"
      placeholder={placeholder}
      aria-label={label}
      value={value}
      onChange={(e) => onChange(e.target.value)}
      className={`tnum ${width} rounded px-2 py-1 text-right text-[13px] outline-none`}
      style={{
        background: "var(--surface-2)",
        border: `1px solid ${bad ? "var(--crit)" : edited ? "var(--accent)" : "var(--line)"}`,
        color: "var(--ink)",
      }}
    />
  );
}

export function FlagInput({
  flag, draft, roster,
}: {
  flag: SprintFlag;
  draft: ChangeDraft;
  roster: StoredPerson[];
}) {
  // What has been typed, kept as text so a half-typed "2." is not
  // rounded away under the person's cursor. The request is derived
  // from it on every change.
  const [typed, setTyped] = useState("");

  if (!flag.key) return null;
  const key = flag.key;

  if (flag.kind === "done_no_estimate") {
    const queued = draft.find(key, "points.set");
    const setPoints = (s: string) => {
      setTyped(s);
      const n = parse(s);
      draft.set(key, "points.set", Number.isNaN(n) || n <= 0 ? null : { key, op: "points.set", points: n });
    };
    // What was typed, or the queued figure where the list re-rendered
    // this row fresh: the request is the truth, the text is its echo.
    const shown = typed !== "" ? typed : queued ? String(queued.request.points ?? "") : "";
    return (
      <span className="flex shrink-0 items-center gap-2">
        <BlankNumberField label={`Story points for ${key}`} placeholder="pts" value={shown} onChange={setPoints} />
        {queued && <Badge tone="info" label="queued" title="Will be in the preview. Clear the field to drop it." />}
      </span>
    );
  }

  if (flag.kind === "done_unassigned") {
    const queued = draft.find(key, "assignee.set");
    const setAssignee = (id: string) =>
      draft.set(key, "assignee.set", id === "" ? null : { key, op: "assignee.set", assignee: id });
    return (
      <span className="flex shrink-0 items-center gap-2">
        <PersonSelect
          roster={roster}
          label={`Assignee for ${key}`}
          value={queued?.request.assignee ?? ""}
          onChange={setAssignee}
        />
        {queued && <Badge tone="info" label="queued" title="Will be in the preview. Choose nobody to drop it." />}
      </span>
    );
  }

  return null;
}
