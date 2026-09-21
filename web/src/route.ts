import { useSyncExternalStore } from "react";
import { TOOLS } from "./tools";

/* The open view lives in the URL.
 *
 * A dashboard people leave open all day gets reloaded constantly, and
 * landing back on the overview every time is the small rudeness that
 * makes a tool feel disposable. The path carries the open tool and the
 * view inside it - /pr/security, /sprint/42 - so a reload, a bookmark
 * and a link pasted to a colleague all arrive at the same place.
 *
 * A real path rather than a hash, because the server already serves
 * index.html for any path it does not recognise, so a deep link survives
 * a cold load. No router either: two segments and the History API are
 * the whole of it.
 */

export type Route = {
  /** Which tool the rail has open. Always one the registry knows. */
  tool: string;
  /** The view inside that tool. Empty means the tool's default; each
   *  tool decides what its views are called, and what an unrecognised
   *  one means - which is always "show the default", never an error. */
  view: string;
};

const DEFAULT: Route = { tool: "pr", view: "" };
const KNOWN = new Set(TOOLS.map((t) => t.id));

// A hand-edited or truncated address can be malformed enough that
// decoding throws. That is a bad link, not a crash.
function decode(segment: string): string {
  try {
    return decodeURIComponent(segment);
  } catch {
    return "";
  }
}

export function parse(url: string): Route {
  const [tool = "", view = ""] = url.split(/[?#]/)[0].replace(/^\/+/, "").split("/");
  const id = decode(tool);
  return KNOWN.has(id) ? { tool: id, view: decode(view) } : DEFAULT;
}

export function path(route: Route): string {
  const view = route.view ? `/${encodeURIComponent(route.view)}` : "";
  return route.tool === DEFAULT.tool && !view ? "/" : `/${route.tool}${view}`;
}

let current = parse(location.pathname);
const listeners = new Set<() => void>();

function set(next: Route) {
  if (next.tool === current.tool && next.view === current.view) return;
  current = next;
  for (const notify of listeners) notify();
}

function onPop() {
  set(parse(location.pathname));
}

function subscribe(notify: () => void): () => void {
  if (listeners.size === 0) window.addEventListener("popstate", onPop);
  listeners.add(notify);
  return () => {
    listeners.delete(notify);
    if (listeners.size === 0) window.removeEventListener("popstate", onPop);
  };
}

/** Open a view. `replace` is for corrections nobody asked for - filling
 *  in a default, say - which have no business in the Back button. */
export function navigate(to: Route, replace = false): void {
  const url = path(to);
  try {
    if (url !== location.pathname) {
      history[replace ? "replaceState" : "pushState"](null, "", url);
    }
  } catch { /* sandboxed frame: the view still moves, the address bar does not */ }
  set(parse(url));
}

/** The route as it stands, re-read on Back, Forward and navigate(). */
export function useRoute(): Route {
  return useSyncExternalStore(subscribe, () => current, () => current);
}
