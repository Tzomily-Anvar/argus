/* Theme presets, carried over from the original Argus.
 *
 * Each preset defines every surface, ink and accent token for both light
 * and dark. Dark is a chosen set of values, not an inversion of light.
 *
 * The semantic colours (critical / warning / good / info) are shared
 * across every preset and deliberately do not change with it: red has to
 * mean the same thing whichever theme you picked. They stay reserved for
 * state and are never reused as decoration - and because two of them are
 * low-contrast on light surfaces, every badge pairs its colour with a
 * glyph and a written label so colour never carries meaning alone.
 */

export type Mode = "light" | "dark";
export type PresetKey = "slate" | "glacier" | "nightfall" | "pine";

type Tokens = Record<string, string>;

export const PRESETS: Record<PresetKey, { name: string; light: Tokens; dark: Tokens }> = {
  slate: {
    name: "Slate",
    light: { ground:"#f3f4f7", surface:"#ffffff", "surface-2":"#e9ebf0", line:"#dadde4", ink:"#1a1d24", muted:"#5b6270", faint:"#8890a0", accent:"#4054b2", "accent-ink":"#ffffff", sidebar:"#1a1c24", "sidebar-ink":"#e7e9f0", "sidebar-muted":"#8890a0" },
    dark:  { ground:"#14161b", surface:"#1b1e25", "surface-2":"#242832", line:"#333846", ink:"#e7e9ee", muted:"#9aa0b0", faint:"#666c7a", accent:"#7c89e6", "accent-ink":"#0e1020", sidebar:"#0d0f14", "sidebar-ink":"#e7e9ee", "sidebar-muted":"#666c7a" },
  },
  glacier: {
    name: "Glacier",
    light: { ground:"#eef5f6", surface:"#ffffff", "surface-2":"#e2eef0", line:"#d3e1e4", ink:"#152225", muted:"#4c6267", faint:"#7f9498", accent:"#0891a8", "accent-ink":"#ffffff", sidebar:"#132226", "sidebar-ink":"#e3eef0", "sidebar-muted":"#7f9498" },
    dark:  { ground:"#0d1517", surface:"#141d20", "surface-2":"#1b262a", line:"#2a3a3f", ink:"#e3eef0", muted:"#93a9ad", faint:"#5e7378", accent:"#3fc4dd", "accent-ink":"#052226", sidebar:"#0a1113", "sidebar-ink":"#e3eef0", "sidebar-muted":"#5e7378" },
  },
  nightfall: {
    name: "Nightfall",
    light: { ground:"#f1f0f7", surface:"#ffffff", "surface-2":"#e6e4f1", line:"#d8d5e6", ink:"#1c1a2b", muted:"#5c5875", faint:"#8a86a3", accent:"#6a4fd6", "accent-ink":"#ffffff", sidebar:"#171529", "sidebar-ink":"#e8e6f3", "sidebar-muted":"#8a86a3" },
    dark:  { ground:"#0f0e19", surface:"#171526", "surface-2":"#1e1c30", line:"#302c47", ink:"#e8e6f3", muted:"#a49fc0", faint:"#65607f", accent:"#9c86f2", "accent-ink":"#140f28", sidebar:"#0a0913", "sidebar-ink":"#e8e6f3", "sidebar-muted":"#65607f" },
  },
  pine: {
    name: "Pine",
    light: { ground:"#eef4f1", surface:"#ffffff", "surface-2":"#e1ebe5", line:"#d1e0d8", ink:"#131f1b", muted:"#4b6459", faint:"#7d968a", accent:"#0f8c6c", "accent-ink":"#ffffff", sidebar:"#0f1e19", "sidebar-ink":"#e2ede7", "sidebar-muted":"#7d968a" },
    dark:  { ground:"#0d1512", surface:"#141e19", "surface-2":"#1a2620", line:"#2a3c33", ink:"#e2ede7", muted:"#8fab9c", faint:"#5a7568", accent:"#4fd6a8", "accent-ink":"#08211a", sidebar:"#080f0c", "sidebar-ink":"#e2ede7", "sidebar-muted":"#5a7568" },
  },
};

export const PRESET_ORDER: PresetKey[] = ["slate", "glacier", "nightfall", "pine"];

export const SEMANTIC: Record<Mode, Tokens> = {
  light: { crit:"#c0392f", "crit-bg":"#f9e4e1", warn:"#a3690a", "warn-bg":"#f6ecd4", good:"#227a4f", "good-bg":"#e2f2e8", info:"#2f5fa8", "info-bg":"#e6edf7" },
  dark:  { crit:"#e8837a", "crit-bg":"#3c211e", warn:"#e0ad5c", "warn-bg":"#3a2e16", good:"#7ecda0", "good-bg":"#1a3324", info:"#8fb2e6", "info-bg":"#1e2c42" },
};

export type Prefs = { preset: PresetKey; mode: Mode };
const DEFAULT_PREFS: Prefs = { preset: "nightfall", mode: "light" };
const STORAGE_KEY = "argus-theme";

export function loadPrefs(): Prefs {
  try {
    return { ...DEFAULT_PREFS, ...JSON.parse(localStorage.getItem(STORAGE_KEY) ?? "{}") };
  } catch {
    return { ...DEFAULT_PREFS };
  }
}

export function savePrefs(p: Prefs): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(p));
  } catch {
    /* private browsing: the theme just does not persist */
  }
}

/** Write a preset's tokens onto <html> as custom properties. */
export function applyTheme(p: Prefs): void {
  const tokens = { ...PRESETS[p.preset][p.mode], ...SEMANTIC[p.mode] };
  const root = document.documentElement;
  for (const [k, v] of Object.entries(tokens)) root.style.setProperty("--" + k, v);
  root.style.colorScheme = p.mode;
}
