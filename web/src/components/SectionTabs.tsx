import type { JSX } from "react";

export type Section = {
  id: string;
  label: string;
  count?: number;
  /** Draw the count in the alert colour, for things genuinely on you. */
  urgent?: boolean;
};

/* Sections belong to the open tool, so they live in the page rather than
 * the rail - the rail is reserved for switching tools. */
export function SectionTabs({
  sections,
  active,
  onSelect,
}: {
  sections: Section[];
  active: string;
  onSelect: (id: string) => void;
}): JSX.Element {
  return (
    <nav
      className="mb-5 flex flex-wrap items-center gap-1 overflow-x-auto"
      style={{ borderBottom: "1px solid var(--line)" }}
    >
      {sections.map((s) => {
        const on = s.id === active;
        return (
          <button
            key={s.id}
            onClick={() => onSelect(s.id)}
            className="relative flex items-center gap-1.5 px-3 py-2 text-[13px] whitespace-nowrap"
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
    </nav>
  );
}
