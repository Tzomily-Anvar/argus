import { useEffect, useLayoutEffect, useMemo, useRef, useState, type CSSProperties } from "react";
import { createPortal } from "react-dom";

/* A searchable select.
 *
 * A board has dozens of sprints, hundreds of epics and more stories, and
 * a native select over any of those is a scroll nobody finishes. This is
 * a button that opens a list with a search box on top, the same shape as
 * the sprint picker's "All sprints" list, so it is already familiar. It
 * opens upward when asked, because the bulk bar sits at the foot of the
 * page and a list dropping below it would drop off the screen.
 *
 * The list is drawn on the page body, placed against the button, rather
 * than inside the button's card: the cards clip what spills out of them,
 * and a picker at a card's right edge would otherwise open half hidden.
 * It keeps to the screen, lining up with the button's right edge when
 * there is no room to its right, and follows the button on scroll. */

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
  const [place, setPlace] = useState<CSSProperties>({});
  const button = useRef<HTMLButtonElement>(null);
  const panel = useRef<HTMLDivElement>(null);
  const input = useRef<HTMLInputElement>(null);

  useLayoutEffect(() => {
    if (!open) return;
    const width = 320;
    const gutter = 8;
    const follow = () => {
      const r = button.current?.getBoundingClientRect();
      if (!r) return;
      setPlace({
        left: r.left + width <= window.innerWidth - gutter ? r.left : Math.max(gutter, r.right - width),
        width,
        ...(up ? { bottom: window.innerHeight - r.top + gutter } : { top: r.bottom + gutter }),
      });
    };
    follow();
    window.addEventListener("scroll", follow, true);
    window.addEventListener("resize", follow);
    return () => {
      window.removeEventListener("scroll", follow, true);
      window.removeEventListener("resize", follow);
    };
  }, [open, up]);

  useEffect(() => {
    if (!open) return;
    input.current?.focus({ preventScroll: true });
    const onDown = (e: MouseEvent) => {
      const t = e.target as Node;
      if (!button.current?.contains(t) && !panel.current?.contains(t)) setOpen(false);
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
    <>
      <button
        ref={button}
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

      {open && createPortal(
        <div
          ref={panel}
          className="fixed z-[60] rounded-xl p-2"
          style={{ ...place, background: "var(--surface)", border: "1px solid var(--line)", boxShadow: "var(--shadow)" }}
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
        </div>,
        document.body,
      )}
    </>
  );
}
