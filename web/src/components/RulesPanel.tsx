import type { RuleInfo } from "../api";

/* Every rule is opt-in/opt-out and tunable without touching code. This
 * panel is generated from the server's registry, so a rule added later
 * documents itself here automatically - including the exact environment
 * variable to set. */

function Value({ value }: { value: unknown }) {
  const text =
    Array.isArray(value) ? (value.length ? value.join(", ") : "(empty)")
    : typeof value === "boolean" ? String(value)
    : value === "" ? "(empty)"
    : String(value);
  return (
    <code className="font-mono text-xs" style={{ color: "var(--color-ink)" }}>
      {text}
    </code>
  );
}

export function RulesPanel({ rules }: { rules: RuleInfo[] }) {
  return (
    <div className="space-y-4">
      <p className="text-sm" style={{ color: "var(--color-ink-muted)" }}>
        Each check is independent. Turn one off or retune it by setting the environment variable
        shown, in your <code className="font-mono text-xs">.env</code>, then restart. Adding a new
        check means adding one file — see CONTRIBUTING.md.
      </p>

      {rules.map((rule) => (
        <article
          key={rule.id}
          className="rounded"
          style={{ background: "var(--color-surface-raised)", boxShadow: "inset 0 0 0 1px var(--color-line)" }}
        >
          <header className="flex flex-wrap items-baseline justify-between gap-2 px-4 pt-3">
            <h3 className="font-semibold">{rule.title}</h3>
            <code className="font-mono text-xs" style={{ color: "var(--color-ink-faint)" }}>
              {rule.id}
            </code>
          </header>

          <div className="space-y-1 px-4 pt-2 pb-3">
            <p className="text-sm">{rule.description}</p>
            <p className="text-sm" style={{ color: "var(--color-ink-muted)" }}>
              {rule.why}
            </p>
          </div>

          <dl className="space-y-2 border-t px-4 py-3 text-sm" style={{ borderColor: "var(--color-line)" }}>
            <div className="flex flex-wrap items-baseline gap-x-2">
              <dt className="font-mono text-xs" style={{ color: "var(--color-ink-faint)" }}>
                {rule.enabled_env}
              </dt>
              <dd>
                <Value value={rule.enabled} />
              </dd>
            </div>
            {rule.params.map((p) => (
              <div key={p.name} className="flex flex-wrap items-baseline gap-x-2">
                <dt className="font-mono text-xs" style={{ color: "var(--color-ink-faint)" }}>
                  {p.env}
                </dt>
                <dd className="flex-1">
                  <Value value={p.value} />
                  <span className="ml-2 text-xs" style={{ color: "var(--color-ink-muted)" }}>
                    {p.desc}
                  </span>
                </dd>
              </div>
            ))}
          </dl>
        </article>
      ))}
    </div>
  );
}
