/* The tool registry.
 *
 * Argus is a shell that hosts tools. Today there is one - pull requests -
 * and the rail exists so the next ones drop in beside it rather than
 * forcing the page to be reshaped.
 *
 * A tool owns the whole content area and defines its own sections, which
 * render as tabs inside it. The rail is never about sections: those
 * belong to whichever tool is open.
 *
 * To add a tool, add an entry here and give it a component. Nothing else
 * in the shell needs to change.
 */

import type { JSX } from "react";
import { Icon } from "./components/Icons";

export type Tool = {
  id: string;
  label: string;
  icon: () => JSX.Element;
  /** Set once the backend for a tool exists; unavailable ones are hidden. */
  available: boolean;
};

export const TOOLS: Tool[] = [
  { id: "pr", label: "Pull requests", icon: Icon.merge, available: true },

  // Planned. Each needs its own backend before it can be switched on, so
  // they stay hidden rather than showing a tab that fails when clicked -
  // the same reasoning the original Argus used when it health-checked
  // each backend before listing it.
  { id: "sprint", label: "Sprint reports", icon: Icon.clock, available: false },
  { id: "backlog", label: "Backlog", icon: Icon.inbox, available: false },
  { id: "notifications", label: "Atlassian", icon: Icon.bell, available: false },
];

export const availableTools = () => TOOLS.filter((t) => t.available);

// The server decides what is actually live: a tool needs credentials, or
// storage, or an explicit ARGUS_TOOLS entry. Merging its answer means an
// unconfigured tool never appears, rather than appearing and failing.
export function mergeAvailability(live: { id: string; available: boolean }[]): Tool[] {
  const byID = new Map(live.map((t) => [t.id, t.available]));
  return TOOLS.filter((t) => byID.get(t.id) ?? false);
}
