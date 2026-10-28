import type { ReactNode } from "react";
import type { Filter, Sort } from "../tableQuery";
import { Picker } from "./backlog/Picker";

/* The controls for sorting and filtering a table, shared by the pull
 * request tables and the backlog's, so both tools behave alike. */

/** A column heading that sorts the table. The arrow says which column
 *  is sorting and which way; aria-sort says it to a screen reader. */
export function SortTh({
  label, col, sort, onSort, startDesc = false, className = "",
}: {
  label: string;
  col: string;
  sort: Sort;
  onSort: (key: string, startDesc?: boolean) => void;
  /** Whether the first click sorts high to low: the oldest, the worst. */
  startDesc?: boolean;
  className?: string;
}) {
  const on = sort?.key === col;
  return (
    <th
      aria-sort={on ? (sort.desc ? "descending" : "ascending") : "none"}
      className={`py-2.5 text-left text-[11px] font-semibold tracking-wide uppercase ${className}`}
      style={{ color: on ? "var(--ink)" : "var(--faint)" }}
    >
      <button
        onClick={() => onSort(col, startDesc)}
        className="inline-flex items-center gap-1 uppercase hover:underline"
        title={on ? "Click to turn the order round, or to go back to the usual order" : `Sort by ${label.toLowerCase()}`}
      >
        {label}
        <span aria-hidden="true" style={{ color: on ? "var(--accent)" : "transparent" }}>
          {on && sort.desc ? "▼" : "▲"}
        </span>
      </button>
    </th>
  );
}

/** A plain heading in a table whose other headings sort. */
export function PlainTh({ children, className = "" }: { children?: ReactNode; className?: string }) {
  return (
    <th className={`py-2.5 text-left text-[11px] font-semibold tracking-wide uppercase ${className}`} style={{ color: "var(--faint)" }}>
      {children}
    </th>
  );
}

export type FilterField = {
  /** The query parameter, such as author. */
  param: string;
  label: string;
  /** What can be picked, with how many rows carry each. */
  options: { value: string; count: number }[];
  /** What a count is of: "pull request", "ticket". */
  noun: string;
};

/** The search box and the pickers above a table. A picked value shows
 *  as a chip with its own ×, and picking it again in the list drops it.
 *  A field with only one value to pick is greyed rather than left out:
 *  it could only ever pick everything, but a bar that changed shape
 *  from tab to tab would read as something gone missing.
 *  A value picked on another tab that this one has no rows for is still
 *  listed, so it can be seen and dropped here. */
export function FilterBar({
  filter, fields, placeholder,
}: {
  filter: Filter;
  fields: FilterField[];
  placeholder: string;
}) {
  const chips = fields.flatMap((f) => (filter.values[f.param] ?? []).map((v) => ({ f, v })));
  return (
    <div className="mb-3 flex flex-wrap items-center gap-2">
      <input
        type="search"
        value={filter.q}
        onChange={(e) => filter.setQ(e.target.value)}
        placeholder={placeholder}
        aria-label="Filter the rows"
        className="w-56 rounded-lg px-2.5 py-1.5 text-[13px] outline-none"
        style={{ background: "var(--surface)", border: "1px solid var(--line)", color: "var(--ink)" }}
      />
      {fields.map((f) => {
        const picked = filter.values[f.param] ?? [];
        // A picker with one thing in it narrows nothing, but it stays in
        // its place, greyed, so the bar keeps one shape on every tab.
        const single = picked.length === 0 && f.options.length < 2;
        const counts = new Map(f.options.map((o) => [o.value, o.count]));
        const values = [...new Set([...picked, ...f.options.map((o) => o.value)])];
        const plural = (n: number) => `${n} ${f.noun}${n === 1 ? "" : "s"}`;
        return (
          <span key={f.param} title={single ? `Only one ${f.label.toLowerCase()} here` : undefined}>
            <Picker
              label={picked.length > 0 ? `${f.label} · ${picked.length}` : f.label}
              items={values.map((v) => ({
                id: v,
                label: v,
                group: picked.includes(v) ? "Picked, pick again to drop" : undefined,
                hint: counts.get(v) ? plural(counts.get(v)!) : "none here",
              })).sort((a, b) => (a.group ? 0 : 1) - (b.group ? 0 : 1))}
              onPick={(v) => filter.toggle(f.param, v)}
              placeholder={`Find ${f.label.toLowerCase()}…`}
              disabled={single}
            />
          </span>
        );
      })}
      {chips.map(({ f, v }) => (
        <span
          key={`${f.param}:${v}`}
          className="inline-flex items-center gap-1 rounded-md px-2 py-0.5 text-[12px]"
          style={{ background: "var(--surface-2)", color: "var(--ink)" }}
        >
          <span style={{ color: "var(--muted)" }}>{f.label.toLowerCase()}:</span> {v}
          <button onClick={() => filter.toggle(f.param, v)} aria-label={`Drop ${v}`} title="Drop" style={{ color: "var(--faint)" }}>×</button>
        </span>
      ))}
      {filter.active && (
        <button onClick={filter.clear} className="lnk text-[12.5px]">Clear filters</button>
      )}
    </div>
  );
}

/** Under a filtered table, how much of it is showing, so a filtered
 *  list is never taken for a short one. Says nothing when the filter
 *  hides nothing. */
export function FilterNote({ shown, total, filter }: { shown: number; total: number; filter: Filter }) {
  if (!filter.active || shown === total) return null;
  return (
    <p className="px-4 py-2 text-[12.5px]" style={{ color: "var(--muted)", borderTop: "1px solid var(--line)" }}>
      {shown === 0 ? `None of ${total} match the filter.` : `${shown} of ${total} shown.`}{" "}
      <button onClick={filter.clear} className="lnk">Clear filters</button>
    </p>
  );
}

/** How many rows carry each value, most first, for a picker's list. */
export function countValues(lists: string[][]): { value: string; count: number }[] {
  const n = new Map<string, number>();
  for (const vs of lists) for (const v of new Set(vs)) if (v) n.set(v, (n.get(v) ?? 0) + 1);
  return [...n].map(([value, count]) => ({ value, count }))
    .sort((a, b) => b.count - a.count || a.value.localeCompare(b.value, undefined, { sensitivity: "base" }));
}
