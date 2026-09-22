import { useEffect, useMemo, useRef, useState } from "react";
import type { SprintOption } from "../api";
import { formatDateRange } from "../dates";

/* The sprint picker.
 *
 * The four most recent are buttons rather than a select, because that is
 * what almost every visit wants and each carries state worth seeing at a
 * glance. Anything older is behind "All sprints", which loads the whole
 * board only when asked - so the common case costs one request and the
 * rare one is still reachable. */
export function SprintPicker({
  sprints,
  all,
  active,
  onPick,
  onNeedAll,
  loading,
  loadingAll,
}: {
  sprints: SprintOption[];
  all: SprintOption[] | null;
  active: number | null;
  onPick: (n: number) => void;
  onNeedAll: () => void;
  loading: boolean;
  loadingAll: boolean;
}) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const panel = useRef<HTMLDivElement>(null);
  const input = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (!open) return;
    onNeedAll();
    input.current?.focus();
    const onDown = (e: MouseEvent) => {
      if (panel.current && !panel.current.contains(e.target as Node)) setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && setOpen(false);
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const matches = useMemo(() => {
    const list = all ?? [];
    const q = query.trim().toLowerCase();
    if (!q) return list;
    return list.filter(
      (s) => String(s.number).includes(q) || s.name.toLowerCase().includes(q),
    );
  }, [all, query]);

  if (loading && sprints.length === 0) {
    return (
      <div className="mb-5 flex gap-2">
        {[0, 1, 2, 3].map((i) => (
          <div key={i} className="h-[52px] w-36 animate-pulse rounded-lg" style={{ background: "var(--surface-2)" }} />
        ))}
      </div>
    );
  }

  const activeIsListed = sprints.some((s) => s.number === active);
  const activeElsewhere = !activeIsListed && active !== null
    ? (all ?? []).find((s) => s.number === active)
    : undefined;

  return (
    <div className="relative mb-5 flex flex-wrap items-stretch gap-2">
      {sprints.map((s) => (
        <SprintButton key={s.jira_id} s={s} on={s.number === active} onPick={onPick} />
      ))}

      {/* A sprint chosen from the full list stays visible beside the
          recent ones, so it never looks as though nothing is selected. */}
      {activeElsewhere && <SprintButton s={activeElsewhere} on onPick={onPick} />}

      <button
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        className="rounded-lg px-3.5 py-2 text-[13px] font-medium"
        style={{
          background: open ? "var(--surface)" : "transparent",
          border: `1px dashed ${open ? "var(--accent)" : "var(--line)"}`,
          color: "var(--muted)",
        }}
      >
        All sprints…
      </button>

      {open && (
        <div
          ref={panel}
          className="absolute top-full left-0 z-50 mt-2 w-80 rounded-xl p-2"
          style={{ background: "var(--surface)", border: "1px solid var(--line)", boxShadow: "var(--shadow)" }}
        >
          <input
            ref={input}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Find a sprint…"
            className="mb-2 w-full rounded-lg px-2.5 py-1.5 text-[13px] outline-none"
            style={{ background: "var(--surface-2)", border: "1px solid var(--line)", color: "var(--ink)" }}
          />

          {loadingAll && (
            <p className="px-2 py-3 text-[13px]" style={{ color: "var(--muted)" }}>
              Loading the board…
            </p>
          )}

          {!loadingAll && matches.length === 0 && (
            <p className="px-2 py-3 text-[13px]" style={{ color: "var(--muted)" }}>
              No sprint matches that.
            </p>
          )}

          <div className="max-h-72 overflow-y-auto">
            {matches.map((s) => (
              <button
                key={s.jira_id}
                onClick={() => {
                  onPick(s.number);
                  setOpen(false);
                  setQuery("");
                }}
                className="flex w-full items-center gap-2 rounded-lg px-2.5 py-2 text-left text-[13px]"
                style={{ background: s.number === active ? "var(--surface-2)" : "transparent" }}
              >
                <span className="font-medium">Sprint {s.number}</span>
                {s.current && (
                  <span className="rounded px-1.5 py-px text-[10px] font-semibold uppercase"
                        style={{ background: "var(--good-bg)", color: "var(--good)" }}>
                    current
                  </span>
                )}
                <span className="ml-auto shrink-0 text-[11.5px] whitespace-nowrap" style={{ color: "var(--muted)" }}>
                  {s.has_report && "stored · "}
                  {formatDateRange(s.starts, s.ends) || s.state}
                </span>
              </button>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

function SprintButton({ s, on, onPick }: { s: SprintOption; on: boolean; onPick: (n: number) => void }) {
  return (
    <button
      onClick={() => onPick(s.number)}
      className="rounded-lg px-3.5 py-2 text-left transition-colors"
      style={{
        background: on ? "var(--surface)" : "transparent",
        border: `1px solid ${on ? "var(--accent)" : "var(--line)"}`,
        boxShadow: on ? "var(--shadow)" : "none",
      }}
    >
      <div className="flex items-center gap-2">
        <span className="text-[14px] font-semibold" style={{ color: "var(--ink)" }}>
          Sprint {s.number}
        </span>
        {s.current && (
          <span className="rounded px-1.5 py-px text-[10px] font-semibold uppercase"
                style={{ background: "var(--good-bg)", color: "var(--good)" }}>
            current
          </span>
        )}
      </div>
      <div className="mt-0.5 flex items-center gap-1.5 text-[11.5px]" style={{ color: "var(--muted)" }}>
        {/* The whole span, not just the end date. A single date on a tab
            reads as "the sprint", and it was the end one, so a sprint that
            ran from late August looked like a sprint in September. The
            year is left off while both ends fall in this one, because
            four repeated digits on every tab is width spent on nothing. */}
        <span className="whitespace-nowrap">{formatDateRange(s.starts, s.ends) || s.state}</span>
        {/* Marked from local storage, so it renders before Jira answers. */}
        {s.has_report && <span title="Already built, so it opens instantly">· stored</span>}
      </div>
    </button>
  );
}
