import type { ReactNode } from "react";
import { Badge, Pill } from "../Badge";

/* The pieces the four table steps are made of.
 *
 * Steps 2 to 5 are the same shape: a table of rows Argus has a suggestion
 * for, each with the suggestion pre-filled, a small label saying where it
 * came from, and three ways out - accept it, change it, or skip it. One
 * copy of that shape, so the steps cannot come to read differently and
 * a person who has learned one has learned them all. */

export const th = "pb-2 pr-3 text-left text-[11px] font-semibold tracking-wide uppercase";
export const small = "rounded px-2 py-0.5 text-[12px] disabled:opacity-40";
export const outlined = { color: "var(--muted)", border: "1px solid var(--line)" } as const;
export const filled = { background: "var(--accent)", color: "#fff" } as const;

/** Which rows the person has decided to leave alone this sitting.
 *
 *  A skip is a decision, not an absence: a row nobody has reached and a
 *  row somebody looked at and left are different, and the count badge on
 *  the step has to tell them apart. Skips are not written to the store,
 *  because the store keeps requests and a skip is the lack of one; they
 *  last as long as the panel is open. */
export type Skips = {
  has: (id: string) => boolean;
  set: (id: string, on: boolean) => void;
};

export function StepIntro({ title, children }: { title: string; children: ReactNode }) {
  return (
    <div className="mb-4">
      <h3 className="text-[15px] font-semibold">{title}</h3>
      <p className="mt-0.5 text-[13px]" style={{ color: "var(--muted)" }}>{children}</p>
    </div>
  );
}

export function Empty({ children }: { children: ReactNode }) {
  return (
    <p className="rounded-lg px-4 py-6 text-center text-[13px]" style={{ background: "var(--surface-2)", color: "var(--muted)" }}>
      {children}
    </p>
  );
}

export function StepTable({ headers, children }: { headers: string[]; children: ReactNode }) {
  return (
    <table className="w-full border-collapse text-[13px]">
      <thead>
        <tr>
          {headers.map((h) => (
            <th key={h} className={th} style={{ color: "var(--faint)" }}>{h}</th>
          ))}
        </tr>
      </thead>
      <tbody>{children}</tbody>
    </table>
  );
}

export function KeyCell({ issueKey, url, summary }: { issueKey: string; url: string; summary: string }) {
  return (
    <td className="py-2 pr-3">
      <a href={url || undefined} target="_blank" rel="noreferrer" className="lnk font-mono text-xs">{issueKey}</a>
      <div className="max-w-56 truncate text-[11.5px]" style={{ color: "var(--faint)" }} title={summary}>{summary}</div>
    </td>
  );
}

/** Where a pre-filled value came from, said in two words beside it. A
 *  suggestion that does not say where it is from is a number nobody can
 *  check, and checking is what this panel is for. */
export function Source({ label, title }: { label: string; title: string }) {
  return <Pill title={title}>{label}</Pill>;
}

/** The three ways out of a row. Which appear depends on where the row
 *  stands: queued rows can be dropped, skipped rows undone, and a row
 *  still open can be accepted or skipped. */
export function RowActions({
  queued, skipped, canAccept, acceptLabel = "Accept", skipLabel = "Skip", onAccept, onSkip, onUndo,
}: {
  queued: boolean;
  skipped: boolean;
  canAccept: boolean;
  acceptLabel?: string;
  skipLabel?: string;
  onAccept: () => void;
  onSkip: () => void;
  onUndo: () => void;
}) {
  if (skipped) {
    return (
      <span className="flex items-center justify-end gap-2">
        <Badge tone="neutral" label="skipped" title="Left alone this sitting. Nothing will be written." />
        <button onClick={onUndo} className={small} style={outlined}>Undo</button>
      </span>
    );
  }
  if (queued) {
    return (
      <span className="flex items-center justify-end gap-2">
        <Badge tone="info" label="queued" title="Will be in the preview. Edit the value to change it." />
        <button onClick={onSkip} className={small} style={outlined}>{skipLabel}</button>
      </span>
    );
  }
  return (
    <span className="flex items-center justify-end gap-2">
      <button onClick={onAccept} disabled={!canAccept} className={`${small} font-medium`} style={filled}>{acceptLabel}</button>
      <button onClick={onSkip} className={small} style={outlined}>{skipLabel}</button>
    </span>
  );
}
