import { useQuery } from "@tanstack/react-query";
import { fetchWrites } from "../../api";
import { Badge, type Tone } from "../Badge";
import { formatDay } from "../../dates";
import { th } from "./Table";

/* The audit log, read-only.
 *
 * One row per attempted write, newest first, whatever came of it. It is
 * here rather than on a page of its own because the question it answers
 * - "what did Argus just do to Jira" - is asked right after pressing
 * Apply, and the reverse is built from these same rows. */

function outcomeTone(outcome: string): Tone {
  switch (outcome) {
    case "applied":
      return "good";
    case "skipped":
      return "warning";
    case "failed":
      return "critical";
    default:
      return "neutral";
  }
}

/** The viewer's clock, because "when did that happen" is asked in the
 *  same sitting; the day-first date is kept for rows from earlier. */
function when(iso: string): string {
  const clock = new Date(iso).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
  return `${formatDay(iso, false)} ${clock}`;
}

function text(v: unknown): string {
  if (v == null || v === "") return "—";
  return typeof v === "string" ? v : JSON.stringify(v);
}

export function WritesLog() {
  const writes = useQuery({ queryKey: ["writes"], queryFn: () => fetchWrites(100) });
  const rows = [...(writes.data?.writes ?? [])].sort((a, b) => (a.at < b.at ? 1 : a.at > b.at ? -1 : 0));

  return (
    <section className="mt-8">
      <h3 className="mb-1 text-[11px] font-semibold tracking-wide uppercase" style={{ color: "var(--faint)" }}>
        Writes
      </h3>
      <p className="mb-2 text-[12.5px]" style={{ color: "var(--muted)" }}>
        Every write Argus has attempted, newest first, with what came of it.
      </p>
      {writes.isLoading && <p className="text-[13px]" style={{ color: "var(--muted)" }}>Reading the log…</p>}
      {writes.error && (
        <p className="text-[13px]" style={{ color: "var(--crit)" }}>{String(writes.error)}</p>
      )}
      {writes.data && rows.length === 0 && (
        <p className="rounded-lg px-4 py-4 text-center text-[13px]" style={{ background: "var(--surface-2)", color: "var(--muted)" }}>
          Nothing has been written yet.
        </p>
      )}
      {rows.length > 0 && (
        <table className="w-full border-collapse text-[12.5px]">
          <thead>
            <tr>
              {["When", "Target", "Operation", "Change", "Outcome", "Actor"].map((h) => (
                <th key={h} className={th} style={{ color: "var(--faint)" }}>{h}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.map((w) => (
              <tr key={String(w.id)} className="border-t align-top" style={{ borderColor: "var(--line)" }}>
                <td className="tnum py-1.5 pr-3 whitespace-nowrap" style={{ color: "var(--muted)" }}>{when(w.at)}</td>
                <td className="py-1.5 pr-3 font-mono text-xs whitespace-nowrap">{w.target}</td>
                <td className="py-1.5 pr-3 whitespace-nowrap">{w.operation}</td>
                <td className="py-1.5 pr-3" style={{ color: "var(--muted)" }}>
                  <span className="tnum">{text(w.before)}</span>
                  <span style={{ color: "var(--faint)" }}> → </span>
                  <span className="tnum" style={{ color: "var(--ink)" }}>{text(w.after)}</span>
                  {w.note && <div className="text-[11.5px]" style={{ color: "var(--faint)" }}>{w.note}</div>}
                </td>
                <td className="py-1.5 pr-3">
                  <Badge tone={outcomeTone(w.outcome)} label={w.outcome || "unknown"} title={w.change_set ? `Change set ${w.change_set}` : undefined} />
                </td>
                <td className="py-1.5 whitespace-nowrap" style={{ color: "var(--muted)" }}>{w.actor || "—"}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  );
}
