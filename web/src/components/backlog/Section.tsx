import type { ReactNode } from "react";

/* A card with a heading, a sentence under it and room on the right for
 * the controls that act on the whole card. The same shape the sprint
 * report's sections have, so a backlog page reads as a page of the same
 * tool. */
export function Section({
  title, hint, right, children,
}: {
  title: ReactNode;
  hint?: ReactNode;
  right?: ReactNode;
  children: ReactNode;
}) {
  return (
    <section className="card mb-4 overflow-hidden">
      <header className="flex flex-wrap items-baseline justify-between gap-2 px-4 pt-3.5 pb-2">
        <div className="min-w-0">
          <h2 className="text-[15px] font-semibold">{title}</h2>
          {hint && (
            <p className="mt-0.5 max-w-2xl text-[13px]" style={{ color: "var(--muted)" }}>{hint}</p>
          )}
        </div>
        {right && <div className="flex flex-wrap items-center gap-2">{right}</div>}
      </header>
      <div style={{ borderTop: "1px solid var(--line)" }}>{children}</div>
    </section>
  );
}

/** ago is how long since a timestamp, in the short form the headers of
 *  every tool use beside "as of". */
export function ago(iso: string): string {
  const secs = (Date.now() - new Date(iso).getTime()) / 1000;
  if (Number.isNaN(secs)) return "";
  if (secs < 60) return "just now";
  const m = Math.floor(secs / 60);
  if (m < 60) return `${m} min ago`;
  const h = Math.floor(m / 60);
  return h < 24 ? `${h}h ago` : `${Math.floor(h / 24)}d ago`;
}
