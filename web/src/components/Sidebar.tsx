import type { JSX } from "react";
import { Icon } from "./Icons";
import { ThemePicker } from "./ThemePicker";
import type { Prefs } from "../theme";

export type NavItem = {
  id: string;
  label: string;
  count?: number;
  icon: () => JSX.Element;
  /** Draw attention only where it is genuinely actionable. */
  urgent?: boolean;
};

/* The rail carries this tool's sections today. It is deliberately a rail
 * rather than a tab strip because the intent is to add further tools
 * beside this one - a second group slots in without reshaping the page. */
export function Sidebar({
  items,
  active,
  onSelect,
  collapsed,
  onToggle,
  user,
  org,
  prefs,
  onPrefs,
}: {
  items: NavItem[];
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

      {items.map((it) => (
        <button
          key={it.id}
          className={`navitem${it.id === active ? " active" : ""}`}
          onClick={() => onSelect(it.id)}
          title={collapsed ? it.label : undefined}
        >
          <it.icon />
          <span className="navlabel">{it.label}</span>
          {it.count !== undefined && it.count > 0 && (
            <span
              className="navbadge"
              style={it.urgent ? { background: "var(--crit)", color: "#fff" } : undefined}
            >
              {it.count}
            </span>
          )}
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
