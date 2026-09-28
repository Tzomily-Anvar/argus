import { useEffect, useMemo, useRef, useState } from "react";

/* A searchable select.
 *
 * A board has dozens of sprints, hundreds of epics and more stories, and
 * a native select over any of those is a scroll nobody finishes. This is
 * a button that opens a list with a search box on top, the same shape as
 * the sprint picker's "All sprints" list, so it is already familiar. It
 * opens upward when asked, because the bulk bar sits at the foot of the
 * page and a list dropping below it would drop off the screen. */

export type PickerItem = { id: string; label: string; hint?: string };

export function Picker({
  label, items, onPick, placeholder = "Find…", up = false, disabled = false,
}: {
  label: string;
  items: PickerItem[];
  onPick: (id: string) => void;
  placeholder?: string;
  up?: boolean;
  disabled?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const panel = useRef<HTMLDivElement>(null);
  const input = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (!open) return;
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
  }, [open]);

  const matches = useMemo(() => {
    const q = query.trim().toLowerCase();
    const list = items ?? [];
    if (!q) return list;
    return list.filter((i) => i.label.toLowerCase().includes(q) || (i.hint ?? "").toLowerCase().includes(q));
  }, [items, query]);

  return (
    <div ref={panel} className="relative">
      <button
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        disabled={disabled}
        className="rounded-lg px-3 py-1.5 text-[13px] font-medium disabled:opacity-40"
        style={{
          background: open ? "var(--surface-2)" : "var(--surface)",
          border: `1px solid ${open ? "var(--accent)" : "var(--line)"}`,
          color: "var(--ink)",
        }}
      >
        {label} <span aria-hidden="true" style={{ color: "var(--faint)" }}>▾</span>
      </button>

      {open && (
        <div
          className={`absolute left-0 z-50 w-80 rounded-xl p-2 ${up ? "bottom-full mb-2" : "top-full mt-2"}`}
          style={{ background: "var(--surface)", border: "1px solid var(--line)", boxShadow: "var(--shadow)" }}
        >
          <input
            ref={input}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={placeholder}
            className="mb-2 w-full rounded-lg px-2.5 py-1.5 text-[13px] outline-none"
            style={{ background: "var(--surface-2)", border: "1px solid var(--line)", color: "var(--ink)" }}
          />
          {matches.length === 0 && (
            <p className="px-2 py-3 text-[13px]" style={{ color: "var(--muted)" }}>Nothing matches that.</p>
          )}
          <div className="max-h-72 overflow-y-auto">
            {matches.map((i) => (
              <button
                key={i.id}
                onClick={() => {
                  onPick(i.id);
                  setOpen(false);
                  setQuery("");
                }}
                className="flex w-full items-center gap-2 rounded-lg px-2.5 py-2 text-left text-[13px]"
                style={{ background: "transparent" }}
              >
                <span className="min-w-0 flex-1 truncate">{i.label}</span>
                {i.hint && (
                  <span className="shrink-0 text-[11.5px] whitespace-nowrap" style={{ color: "var(--muted)" }}>{i.hint}</span>
                )}
              </button>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
