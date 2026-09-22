/* Dates, in one place and day first.
 *
 * Every date on these pages used to be an ISO string sliced to ten
 * characters, which is unambiguous and reads like a database. The
 * alternative most codebases reach for is toLocaleDateString with no
 * locale argument, and that is worse than it looks: with no locale it
 * follows the VIEWER's machine, so the same sprint reads "27 Aug" to one
 * colleague and "Aug 27" to another, and a screenshot of a report cannot
 * be trusted to mean what it appears to mean.
 *
 * So the format is pinned here rather than inherited. Day first, because
 * that is how this team writes dates, and a short month name because
 * 09/10 is two different days depending on who is reading it.
 *
 * The calendar date is taken from the characters of the timestamp rather
 * than through Date, deliberately. Jira sends sprint boundaries at
 * midnight UTC, and reading one through the browser's timezone would move
 * it to the previous day for anybody west of London. The date a sprint
 * started is a fact about the sprint, not about where you opened it.
 */

const MONTHS = [
  "Jan", "Feb", "Mar", "Apr", "May", "Jun",
  "Jul", "Aug", "Sep", "Oct", "Nov", "Dec",
];

type Parts = { day: number; month: number; year: number };

/** parts reads the calendar date out of an ISO timestamp.
 *
 *  Returns null for anything unusable, including Go's zero time. A zero
 *  time arrives as a real string - "0001-01-01T00:00:00Z" - so a plain
 *  truthiness check treats "no date at all" as the first of January in
 *  the year one, and renders it. */
function parts(iso?: string | null): Parts | null {
  if (!iso) return null;
  const m = /^(\d{4})-(\d{2})-(\d{2})/.exec(iso);
  if (!m) return null;
  const year = Number(m[1]);
  if (year < 1900) return null;
  return { year, month: Number(m[2]), day: Number(m[3]) };
}

function month(p: Parts): string {
  return MONTHS[p.month - 1] ?? String(p.month);
}

/** formatDay is one date: "27 Aug" or "27 Aug 2026". */
export function formatDay(iso?: string | null, withYear = true): string {
  const p = parts(iso);
  if (!p) return "";
  return withYear ? `${p.day} ${month(p)} ${p.year}` : `${p.day} ${month(p)}`;
}

/** formatDateRange is a sprint's span, as "27 Aug – 10 Sep".
 *
 *  The year is shown whenever leaving it out would be ambiguous: when the
 *  two dates fall in different years, or in a year that is not this one.
 *  Otherwise it is left off, because four digits repeated on every tab is
 *  four digits nobody reads.
 *
 *  With only one of the two dates it says which end it has rather than
 *  printing a dash with nothing beside it. Jira allows a sprint with no
 *  start or no end, and half a range is still worth showing. */
export function formatDateRange(
  startsISO?: string | null,
  endsISO?: string | null,
  alwaysYear = false,
): string {
  const a = parts(startsISO);
  const b = parts(endsISO);
  if (!a && !b) return "";
  if (!a) return `to ${formatDay(endsISO, alwaysYear || b!.year !== thisYear())}`;
  if (!b) return `from ${formatDay(startsISO, alwaysYear || a.year !== thisYear())}`;

  // Different years: both need one, or "27 Dec – 8 Jan" hides the turn of
  // the year that makes the sprint look impossible.
  if (a.year !== b.year) {
    return `${formatDay(startsISO)} – ${formatDay(endsISO)}`;
  }
  const withYear = alwaysYear || a.year !== thisYear();
  return `${formatDay(startsISO, false)} – ${formatDay(endsISO, withYear)}`;
}

function thisYear(): number {
  return new Date().getFullYear();
}
