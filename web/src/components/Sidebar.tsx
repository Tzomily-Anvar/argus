import { Icon } from "./Icons";
import { ThemePicker } from "./ThemePicker";
import type { Prefs } from "../theme";
import type { Tool } from "../tools";

/* The rail switches between tools, and nothing else.
 *
 * Sections belong to whichever tool is open and render as tabs inside the
 * page. Keeping that separation is what lets a second tool be added
 * without reshaping anything. */
export function Sidebar({
  tools,
  active,
  onSelect,
  collapsed,
  onToggle,
  user,
  org,
  prefs,
  onPrefs,
}: {
  tools: Tool[];
  active: string;
  onSelect: (id: string) => void;
  collapsed: boolean;
  onToggle: () => void;
  user?: string;
  org?: string;
  prefs: Prefs;
  onPrefs: (p: Prefs) => void;
}) {
  return (
    <nav className="rail">
      <div className="relative flex items-center gap-2.5 px-2 pt-1.5 pb-5">
        <span style={{ color: "var(--accent)" }}>
          <Icon.eye />
        </span>
        <b className="navlabel text-[14.5px] font-semibold tracking-tight" style={{ color: "var(--sidebar-ink)" }}>
          Argus
        </b>
        <button
          onClick={onToggle}
          aria-label={collapsed ? "Expand sidebar" : "Collapse sidebar"}
          className="ml-auto flex size-[22px] shrink-0 items-center justify-center rounded-md"
          style={{ color: "var(--sidebar-muted)", background: "none", border: "none", cursor: "pointer" }}
        >
          <span style={{ display: "inline-flex", transform: collapsed ? "rotate(180deg)" : "none", transition: "transform .15s ease" }}>
            <Icon.chevron />
          </span>
        </button>
      </div>

      {tools.map((t) => (
        <button
          key={t.id}
          className={`navitem${t.id === active ? " active" : ""}`}
          onClick={() => onSelect(t.id)}
          title={collapsed ? t.label : undefined}
        >
          <t.icon />
          <span className="navlabel">{t.label}</span>
        </button>
      ))}

      <div className="mt-auto flex flex-col gap-0.5">
        {user && (
          <div className="navlabel px-2.5 py-2" style={{ borderTop: "1px solid rgb(255 255 255 / 0.1)" }}>
            <div className="text-[12.5px] font-semibold" style={{ color: "var(--sidebar-ink)" }}>
              {user}
            </div>
            <div className="mt-px text-[11px]" style={{ color: "var(--sidebar-muted)" }}>
              {org}
            </div>
          </div>
        )}
        <ThemePicker prefs={prefs} onChange={onPrefs} collapsed={collapsed} />
      </div>
    </nav>
  );
}
