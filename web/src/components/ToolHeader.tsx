import type { ComponentProps, ReactNode } from "react";

/* The strip at the top of every tool: its name, a line under it, and on
 * the right the "as of" note with the buttons that act on the whole
 * page. One shape, so moving between the pull requests, the sprint
 * report and the backlog does not move the Refresh button. */

export function ToolHeader({
  title, lede, subtitle, note, actions,
}: {
  title: ReactNode;
  /** A sentence in full ink before the subtitle, for a page whose
   *  purpose needs saying before what it shows. */
  lede?: ReactNode;
  subtitle?: ReactNode;
  /** When the page was last built, or that it is being built now. */
  note?: ReactNode;
  actions?: ReactNode;
}) {
  return (
    <header className="mb-5 flex flex-wrap items-center justify-between gap-3">
      <div>
        <h1 className="text-lg font-semibold tracking-tight">{title}</h1>
        {lede && <p className="mt-0.5 text-[13px]" style={{ color: "var(--ink)" }}>{lede}</p>}
        {subtitle && <p className="mt-0.5 text-[13px]" style={{ color: "var(--muted)" }}>{subtitle}</p>}
      </div>
      {(note !== undefined || actions) && (
        <div className="flex items-center gap-2">
          {note !== undefined && <span className="text-xs" style={{ color: "var(--faint)" }}>{note}</span>}
          {actions}
        </div>
      )}
    </header>
  );
}

/** A header button: raised, so it reads as chrome rather than as part
 *  of the page. iconOnly is the gear, square around its glyph. */
export function HeaderButton({ iconOnly = false, children, ...rest }: ComponentProps<"button"> & { iconOnly?: boolean }) {
  return (
    <button
      {...rest}
      className={iconOnly
        ? "flex items-center rounded-lg px-2.5 py-2 text-sm font-medium"
        : "flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-sm font-medium disabled:opacity-50"}
      style={{ background: "var(--surface)", border: "1px solid var(--line)", boxShadow: "var(--shadow)" }}
    >
      {children}
    </button>
  );
}
