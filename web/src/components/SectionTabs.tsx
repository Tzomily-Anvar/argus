import { useEffect, useRef, useState, type JSX } from "react";

export type Section = {
  id: string;
  label: string;
  count?: number;
  /** Draw the count in the alert colour, for things genuinely on you. */
  urgent?: boolean;
};

/* Sections belong to the open tool, so they live in the page rather than
 * the rail - the rail is reserved for switching tools.
 *
 * One row, always. A strip that wraps puts the last tab on a line of its
 * own, where it reads as a different kind of thing. When the row is
 * wider than the page it scrolls sideways instead, with a fade on
 * whichever edge has more behind it, and the active tab is scrolled into
 * view so a deep link never lands on a tab you cannot see. */
export function SectionTabs({
  sections,
  active,
  onSelect,
}: {
  sections: Section[];
  active: string;
  onSelect: (id: string) => void;
}): JSX.Element {
  const strip = useRef<HTMLDivElement>(null);
  const [fade, setFade] = useState({ left: false, right: false });

  // Which edges have more behind them. Re-judged on scroll and resize,
  // and whenever the set of tabs changes.
  useEffect(() => {
    const el = strip.current;
    if (!el) return;
    const judge = () => setFade({
      left: el.scrollLeft > 1,
      right: el.scrollLeft + el.clientWidth < el.scrollWidth - 1,
    });
    judge();
    el.addEventListener("scroll", judge, { passive: true });
    const ro = new ResizeObserver(judge);
    ro.observe(el);
    return () => { el.removeEventListener("scroll", judge); ro.disconnect(); };
  }, [sections.length]);

  useEffect(() => {
    strip.current?.querySelector<HTMLElement>(`[data-tab="${active}"]`)
      ?.scrollIntoView({ block: "nearest", inline: "nearest" });
  }, [active]);

  const edge = (side: "left" | "right") => (
    <span
      aria-hidden="true"
      className={`pointer-events-none absolute top-0 bottom-px w-8 ${side === "left" ? "left-0" : "right-0"}`}
      style={{ background: `linear-gradient(to ${side === "left" ? "right" : "left"}, var(--ground), transparent)` }}
    />
  );

  return (
    <nav className="relative mb-5" style={{ borderBottom: "1px solid var(--line)" }}>
      <div ref={strip} className="no-scrollbar flex items-center gap-0.5 overflow-x-auto">
        {sections.map((s) => {
          const on = s.id === active;
          return (
            <button
              key={s.id}
              data-tab={s.id}
              onClick={() => onSelect(s.id)}
              className="relative flex shrink-0 items-center gap-1.5 px-2.5 py-2 text-[13px] whitespace-nowrap"
              style={{
                color: on ? "var(--ink)" : "var(--muted)",
                fontWeight: on ? 600 : 500,
              }}
            >
              {s.label}
              {s.count !== undefined && s.count > 0 && (
                <span
                  className="tnum rounded-full px-1.5 py-px text-[10.5px] font-semibold"
                  style={{
                    background: s.urgent ? "var(--crit-bg)" : "var(--surface-2)",
                    color: s.urgent ? "var(--crit)" : "var(--muted)",
                  }}
                >
                  {s.count}
                </span>
              )}
              {on && (
                <span
                  className="absolute right-0 -bottom-px left-0 h-0.5 rounded-t"
                  style={{ background: "var(--accent)" }}
                />
              )}
            </button>
          );
        })}
      </div>
      {fade.left && edge("left")}
      {fade.right && edge("right")}
    </nav>
  );
}
