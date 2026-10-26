import type { BacklogLabels, LabelStyle } from "./api";
import type { PickerItem } from "./components/backlog/Picker";

/* Label spelling, as the server has it in internal/backlog/labels.go,
 * for the one place it is needed before anything is sent: a label being
 * typed into a picker. Kept to the same rules so that what the picker
 * suggests is what the Labels view would ask for afterwards. Lower-casing
 * can still differ from Go's in a few scripts (a Greek final sigma), so
 * the server's findings are the word that counts. */

const alnum = /[\p{L}\p{Nd}]/u;
const upper = (c: string) => c !== c.toLowerCase() && c === c.toUpperCase();
const lower = (c: string) => c !== c.toUpperCase() && c === c.toLowerCase();
const digit = (c: string) => /\p{Nd}/u.test(c);

/** A label's words: split at anything not a letter or digit, where a
 *  lower-case letter or digit meets a capital, and before the last
 *  capital of a run followed by lower case (APIGateway is API, Gateway). */
export function labelWords(label: string): string[] {
  const cs = [...label];
  const words: string[] = [];
  let word = "";
  cs.forEach((c, i) => {
    if (!alnum.test(c)) {
      if (word) words.push(word);
      word = "";
      return;
    }
    const prev = cs[i - 1] ?? "";
    const next = cs[i + 1] ?? "";
    if (word && upper(c) && (lower(prev) || digit(prev) || (upper(prev) && lower(next)))) {
      words.push(word);
      word = "";
    }
    word += c;
  });
  if (word) words.push(word);
  return words;
}

/** What two spellings of one label share: letters and digits, lower case. */
export const labelKey = (label: string) => [...label].filter((c) => alnum.test(c)).join("").toLowerCase();

/** A label written in a style; any leaves it as it is. Train keeps the
 *  capitals inside a word, so an acronym stays one; the rest lower-case
 *  every word first. */
export function spellLabel(label: string, style: LabelStyle): string {
  const words = labelWords(label).map((w) => (style === "train" ? w : w.toLowerCase()));
  if (style === "any" || words.length === 0) return label;
  // The first character upper-cased only where it stays one character,
  // as Go's unicode.ToUpper does: ß stays ß rather than becoming SS.
  const cap = (w: string) => {
    const [first, ...rest] = [...w];
    const up = first.toUpperCase();
    return ([...up].length === 1 ? up : first) + rest.join("");
  };
  switch (style) {
    case "train": return words.map(cap).join("-");
    case "kebab": return words.join("-");
    case "snake": return words.join("_");
    case "camel": return words.map((w, i) => (i === 0 ? w : cap(w))).join("");
    case "pascal": return words.map(cap).join("");
  }
}

export const STYLE_NAMES: Record<LabelStyle, string> = {
  any: "any", kebab: "kebab-case", snake: "snake_case", camel: "camelCase", pascal: "PascalCase", train: "Train-Case",
};

const tickets = (n: number) => `${n} ${n === 1 ? "ticket" : "tickets"}`;

/** The label pickers' list, in groups: the chosen ones, the active
 *  repositories, the rest in use, and the rest of the site. A label is
 *  listed once, in the first group it belongs to. */
export function labelItems(data: BacklogLabels | undefined, skip: string[] = []): PickerItem[] {
  if (!data) return [];
  const counts = new Map((data.in_use ?? []).map((l) => [l.name, l.count]));
  const seen = new Set(skip);
  const out: PickerItem[] = [];
  const add = (name: string, group: string, hint?: string) => {
    if (seen.has(name)) return;
    seen.add(name);
    out.push({ id: name, label: name, group, hint });
  };
  for (const l of data.labels ?? []) {
    const n = counts.get(l.name);
    add(l.name, "Chosen", n ? tickets(n) : undefined);
  }
  for (const r of data.repos ?? []) {
    add(r.label, "Repositories", r.count ? `${r.repo} · ${tickets(r.count)}` : r.repo === r.label ? "not used yet" : `${r.repo} · not used yet`);
  }
  for (const l of data.in_use ?? []) add(l.name, "In use", tickets(l.count));
  for (const l of data.elsewhere ?? []) add(l, "Elsewhere in Jira", "not on the backlog");
  return out;
}

/** Whether two keys are one edit apart: a letter added, dropped or
 *  changed. Short keys are left alone, since every two-letter label is
 *  one edit from dozens of others. */
function oneEdit(a: string, b: string): boolean {
  if (a === b || Math.min(a.length, b.length) < 4 || Math.abs(a.length - b.length) > 1) return false;
  let i = 0;
  while (i < a.length && i < b.length && a[i] === b[i]) i++;
  let j = 0;
  while (j < a.length - i && j < b.length - i && a[a.length - 1 - j] === b[b.length - 1 - j]) j++;
  return a.length - i - j <= 1 && b.length - i - j <= 1;
}

/** What to offer for a label typed into a picker that matches none
 *  exactly: an existing label it is a spelling of, which is offered
 *  instead of creating it; otherwise a new one in the team's style,
 *  with the spelling as typed second, and any label one letter away
 *  under them both. Jira creates a label on first use, so a new label's
 *  id is simply its name. */
export function newLabelItems(typed: string, known: string[], style: LabelStyle): PickerItem[] {
  const t = typed.trim();
  if (!t || known.includes(t)) return [];
  const key = labelKey(t);
  if (!key) return [{ id: "", label: "A label needs a letter or a digit", group: "New label", disabled: true }];

  const same = known.filter((k) => labelKey(k) === key);
  if (same.length > 0) {
    return same.map((k) => ({ id: k, label: k, group: "Already exists", hint: "same label, spelt differently" }));
  }

  // Jira refuses a label with a space in it, so typed words are joined:
  // in the team's style, or where there is none, in kebab-case. The new
  // label comes first, so Enter creates what was typed.
  const styled = spellLabel(t, style === "any" && /\s/.test(t) ? "kebab" : style);
  const out: PickerItem[] = [
    { id: styled, label: `Create ${styled}`, group: "New label", hint: style === "any" ? "new label" : STYLE_NAMES[style] },
  ];
  if (styled !== t && !/\s/.test(t)) {
    out.push({ id: t, label: `Create ${t}`, group: "New label", hint: "as typed, not the team's style" });
  }
  return out.concat(known
    .filter((k) => oneEdit(labelKey(k), key))
    .slice(0, 3)
    .map((k) => ({ id: k, label: k, group: "Did you mean", hint: "one letter away" })));
}
