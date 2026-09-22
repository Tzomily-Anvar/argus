import { useState } from "react";

/* The pieces both editing panels are made of.
 *
 * There are two panels over the sprint report and they edit two different
 * kinds of thing - durable setup behind the gear, this sprint's capacity
 * beside it - but they must behave identically, because the thing being
 * learned is "typing does not save, Save saves". Two copies of that
 * behaviour would drift, and the first difference would teach somebody
 * that one of the panels writes as you type.
 *
 * Editing is explicit everywhere. Nothing is written as you type: what
 * you type stays on the screen until you press Save, and the panel says
 * plainly whether it is saved, unsaved or failed. Saving on blur looked
 * broken for a good reason - it put the typed value back to the server's
 * while the write was in flight, and the server's answer arrived seconds
 * later with the new one, so a number appeared to reset itself and then
 * change its mind. */

/** parse returns NaN for anything that is not a usable number, including
 *  a cleared field, so Save can refuse rather than write a zero nobody
 *  asked for. */
export function parse(s: string): number {
  if (s.trim() === "") return NaN;
  const n = Number(s);
  return n < 0 ? NaN : n;
}

export function NumberField({
  value, draft, onChange, disabled, width = "w-16", step = "0.5", label,
}: {
  value: number;
  draft?: string;
  onChange: (s: string) => void;
  disabled?: boolean;
  width?: string;
  step?: string;
  label: string;
}) {
  const shown = draft ?? String(value);
  const edited = draft !== undefined;
  const bad = edited && Number.isNaN(parse(draft));
  return (
    <input
      type="number"
      step={step}
      min="0"
      aria-label={label}
      disabled={disabled}
      value={shown}
      onChange={(e) => onChange(e.target.value)}
      className={`tnum ${width} rounded px-2 py-1 text-right text-[13px] outline-none disabled:opacity-50`}
      style={{
        background: "var(--surface-2)",
        border: `1px solid ${bad ? "var(--crit)" : edited ? "var(--accent)" : "var(--line)"}`,
        color: "var(--ink)",
      }}
    />
  );
}

export type SaveState = "idle" | "pending" | "saved" | "error";

/** stateOf reads a mutation's three flags as the one state a person sees. */
export function stateOf(m: { isPending: boolean; isError: boolean; isSuccess: boolean }): SaveState {
  return m.isPending ? "pending" : m.isError ? "error" : m.isSuccess ? "saved" : "idle";
}

/** A Save button that says what it is doing. Disabled because there is
 *  nothing to save is a different thing from a save that failed, and the
 *  two must not look the same. */
export function SaveBar({
  dirty, invalid, state, error, onSave, children,
}: {
  dirty: number;
  invalid: boolean;
  state: SaveState;
  error?: string;
  onSave: () => void;
  children?: React.ReactNode;
}) {
  const note =
    state === "pending" ? "Saving…"
      : state === "error" ? (error ?? "Could not save")
        : invalid ? "One of those is not a number"
          : dirty > 0 ? `${dirty} unsaved ${dirty === 1 ? "change" : "changes"}`
            : state === "saved" ? "Saved"
              : "Nothing to save";

  const tone =
    state === "error" || invalid ? "var(--crit)"
      : dirty > 0 ? "var(--warn)"
        : state === "saved" ? "var(--good)"
          : "var(--faint)";

  return (
    <div
      className="flex flex-wrap items-center justify-between gap-2 px-5 py-3"
      style={{ borderTop: "1px solid var(--line)", background: "var(--surface)" }}
    >
      <span className="text-[12.5px]" style={{ color: tone }}>{note}</span>
      <div className="flex items-center gap-2">
        {children}
        <button
          onClick={onSave}
          disabled={dirty === 0 || invalid || state === "pending"}
          className="rounded-lg px-3 py-1.5 text-sm font-medium disabled:opacity-40"
          style={{ background: "var(--accent)", color: "#fff" }}
        >
          {state === "pending" ? "Saving…" : "Save"}
        </button>
      </div>
    </div>
  );
}

/** The panel itself: a sheet over the report, which refuses to close on
 *  unsaved work without saying so first. Closing silently is the one way
 *  an explicit Save can still lose what you typed. */
export function SidePanel({
  title, subtitle, pending, onClose, children, footer, banner,
}: {
  title: string;
  subtitle?: string;
  pending: boolean;
  onClose: () => void;
  children: React.ReactNode;
  footer: React.ReactNode;
  banner?: React.ReactNode;
}) {
  const [confirmClose, setConfirmClose] = useState(false);

  const attemptClose = () => {
    if (pending) {
      setConfirmClose(true);
      return;
    }
    onClose();
  };

  return (
    <>
      <div onClick={attemptClose} className="fixed inset-0 z-40" style={{ background: "rgb(0 0 0 / 0.18)" }} />

      <aside
        className="fixed top-0 right-0 z-50 flex h-full w-[620px] max-w-[94vw] flex-col"
        style={{ background: "var(--surface)", borderLeft: "1px solid var(--line)", boxShadow: "var(--shadow)" }}
      >
        <header className="flex items-start justify-between gap-3 px-5 pt-5 pb-3">
          <div>
            <h2 className="text-[15px] font-semibold">{title}</h2>
            {subtitle && (
              <p className="mt-0.5 text-[12.5px]" style={{ color: "var(--muted)" }}>{subtitle}</p>
            )}
          </div>
          <button
            onClick={attemptClose}
            className="shrink-0 rounded-lg px-2.5 py-1 text-sm"
            style={{ color: "var(--muted)", border: "1px solid var(--line)" }}
          >
            Done
          </button>
        </header>

        {confirmClose && (
          <div
            className="mx-5 mb-3 flex flex-wrap items-center gap-2 rounded-lg px-3 py-2 text-[12.5px]"
            style={{ background: "var(--warn-bg)", color: "var(--warn)" }}
          >
            <span className="flex-1">Close without saving? Those changes will be lost.</span>
            <button
              onClick={() => setConfirmClose(false)}
              className="rounded px-2 py-1 font-medium"
              style={{ border: "1px solid var(--warn)" }}
            >
              Keep editing
            </button>
            <button
              onClick={onClose}
              className="rounded px-2 py-1 font-medium"
              style={{ border: "1px solid var(--warn)" }}
            >
              Discard
            </button>
          </div>
        )}

        {banner}

        <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">{children}</div>

        {footer}
      </aside>
    </>
  );
}
