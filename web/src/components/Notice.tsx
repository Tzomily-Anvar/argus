import type { ReactNode } from "react";

/* A banner for something that went wrong or needs saying before the
 * figures beneath it are read: an error from the server, a warning
 * that a sweep was partial, or a note on what a write is about to do.
 * The same three colours wherever it appears, so a reader learns once
 * what red, amber and blue mean here.
 *
 * Two sizes: "page" sits between a tool's header and its content, and
 * "panel" fits inside a side panel, where the type is a point smaller. */

const TONE = {
  crit: { background: "var(--crit-bg)", color: "var(--crit)" },
  warn: { background: "var(--warn-bg)", color: "var(--warn)" },
  info: { background: "var(--info-bg)", color: "var(--info)" },
} as const;

const LAYOUT = {
  page: "rounded-xl px-3.5 py-2.5 text-sm",
  panel: "rounded-lg px-3 py-2 text-[13px]",
} as const;

export function Notice({
  tone = "crit", layout = "page", className = "", children,
}: {
  tone?: keyof typeof TONE;
  layout?: keyof typeof LAYOUT;
  className?: string;
  children: ReactNode;
}) {
  return (
    <p className={`${LAYOUT[layout]} ${className}`.trim()} style={TONE[tone]}>
      {children}
    </p>
  );
}

/** errorText is the words for whatever a query or mutation failed with.
 *  Fetches throw an Error carrying the server's message; anything else
 *  is shown as it is, and nothing at all reads as an empty string. */
export function errorText(e: unknown): string {
  return e instanceof Error ? e.message : e ? String(e) : "";
}
