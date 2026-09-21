import { useState } from "react";
import type { PolicyHint as Hint } from "../api";

/* Shown when a check fails on nearly every open pull request, which is
 * the signature of a policy gate rather than a broken build.
 *
 * This is a suggestion, not a correction: nothing in the table above has
 * been reclassified. It exists because a fresh install cannot ship a
 * default list of gate names without guessing at someone else's CI, and
 * without one the first run reports almost everything as broken. */

export function PolicyHintBanner({ hints }: { hints: Hint[] }) {
  const [dismissed, setDismissed] = useState<string[]>(() => {
    try {
      return JSON.parse(localStorage.getItem("argus-dismissed-hints") ?? "[]");
    } catch {
      return [];
    }
  });

  const live = hints.filter((h) => !dismissed.includes(h.name));
  if (live.length === 0) return null;

  const dismiss = (name: string) => {
    const next = [...dismissed, name];
    setDismissed(next);
    try {
      localStorage.setItem("argus-dismissed-hints", JSON.stringify(next));
    } catch {
      /* private browsing: it simply reappears next time */
    }
  };

  return (
    <div className="mb-3 space-y-2">
      {live.map((h) => (
        <div
          key={h.name}
          className="rounded p-3 text-sm"
          style={{ background: "var(--color-surface-sunken)", boxShadow: "inset 0 0 0 1px var(--color-line)" }}
        >
          <div className="flex items-start justify-between gap-3">
            <p>
              <code className="font-mono text-xs">{h.name}</code> is failing on{" "}
              <span className="tnum font-semibold">
                {h.failing_on} of {h.out_of}
              </span>{" "}
              open pull requests. That is usually a policy gate, not a broken build.
            </p>
            <button
              onClick={() => dismiss(h.name)}
              aria-label={`Dismiss the suggestion about ${h.name}`}
              className="shrink-0 rounded px-1.5 text-xs"
              style={{ color: "var(--color-ink-faint)" }}
            >
              Dismiss
            </button>
          </div>
          <p className="mt-2 text-xs" style={{ color: "var(--color-ink-muted)" }}>
            To report it separately instead of as a failure, add this to your{" "}
            <code className="font-mono">.env</code> and restart:
          </p>
          <pre
            className="mt-1 overflow-x-auto rounded p-2 font-mono text-xs"
            style={{ background: "var(--color-surface)", color: "var(--color-ink)" }}
          >
            {h.env}={h.token}
          </pre>
        </div>
      ))}
    </div>
  );
}
