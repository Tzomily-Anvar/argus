import { editSearch, useSearch } from "./route";

/* How a table is sorted and filtered, kept in the query string.
 *
 * One sort and one filter per tool, not per table: a person looking for
 * one author's pull requests is looking for them on every tab, and a
 * filter that quietly fell away on switching tabs would read as those
 * pull requests having gone. The parameters are plain words -
 * ?sort=author, ?sort=-age, ?author=a&author=b, ?q=login - so an address
 * can be read, edited and pasted by hand.
 *
 * A sort is a column and a direction. Each column has the direction it
 * starts in, the one a person usually wants first: names A to Z, the
 * oldest first, the worst first. A second click turns it round and a
 * third goes back to the order the list arrives in, which is the order
 * the server chose and is often the useful one. */

export type Sort = { key: string; desc: boolean } | null;

export function useSort(): [Sort, (key: string, startDesc?: boolean) => void] {
  const params = useSearch();
  const raw = params.get("sort") ?? "";
  const sort: Sort = raw ? { key: raw.replace(/^-/, ""), desc: raw.startsWith("-") } : null;
  const cycle = (key: string, startDesc = false) => {
    let next: Sort;
    if (sort?.key !== key) next = { key, desc: startDesc };
    else if (sort.desc === startDesc) next = { key, desc: !startDesc };
    else next = null;
    editSearch((p) => {
      if (next) p.set("sort", `${next.desc ? "-" : ""}${next.key}`);
      else p.delete("sort");
    });
  };
  return [sort, cycle];
}

/** Rows in a sort's order, by the comparison named for its column. An
 *  unknown column, from an address written for another table, leaves the
 *  order alone. Ties keep the order the rows came in. */
export function sortRows<T>(rows: T[], sort: Sort, by: Record<string, (a: T, b: T) => number>): T[] {
  const cmp = sort ? by[sort.key] : undefined;
  if (!sort || !cmp) return rows;
  return [...rows].sort((a, b) => (sort.desc ? -cmp(a, b) : cmp(a, b)));
}

/** Text in reading order: case aside, and numbers by value, so that
 *  ABC-9 comes before ABC-10. */
export const byText = (a: string, b: string) => a.localeCompare(b, undefined, { numeric: true, sensitivity: "base" });

export type Filter = {
  /** What was typed in the search box. */
  q: string;
  /** The values picked for each field; empty when none are. */
  values: Record<string, string[]>;
  /** Whether anything at all is filtering the rows. */
  active: boolean;
  setQ: (q: string) => void;
  toggle: (field: string, value: string) => void;
  clear: () => void;
};

/** The filter, read from the query string. `fields` are the parameters
 *  a value can be picked for; the search box is always `q`. */
export function useFilter(fields: string[]): Filter {
  const params = useSearch();
  const q = params.get("q") ?? "";
  const values = Object.fromEntries(fields.map((f) => [f, params.getAll(f)]));
  return {
    q,
    values,
    active: q.trim() !== "" || fields.some((f) => values[f].length > 0),
    setQ: (text) => editSearch((p) => { if (text) p.set("q", text); else p.delete("q"); }),
    toggle: (field, value) => editSearch((p) => {
      const now = p.getAll(field);
      p.delete(field);
      for (const v of now.includes(value) ? now.filter((x) => x !== value) : [...now, value]) p.append(field, v);
    }),
    clear: () => editSearch((p) => { p.delete("q"); for (const f of fields) p.delete(f); }),
  };
}

/** A filter with some fields set aside, for a table where they cannot
 *  narrow anything: the author on a table of your own pull requests.
 *  They stay in the address, for the tabs where they do. */
export function without(filter: Filter, fields: string[]): Filter {
  const values = Object.fromEntries(Object.entries(filter.values).filter(([f]) => !fields.includes(f)));
  return {
    ...filter,
    values,
    active: filter.q.trim() !== "" || Object.values(values).some((v) => v.length > 0),
  };
}

/** Whether a row passes a filter: every field with values picked must
 *  hold one of them, and the search must appear in one of `text`. A
 *  field a row has several values for, such as labels, passes when any
 *  of them is picked. */
export function passes(filter: Filter, has: Record<string, string[]>, text: string[]): boolean {
  for (const [field, picked] of Object.entries(filter.values)) {
    if (picked.length > 0 && !(has[field] ?? []).some((v) => picked.includes(v))) return false;
  }
  const q = filter.q.trim().toLowerCase();
  return !q || text.some((t) => t.toLowerCase().includes(q));
}
