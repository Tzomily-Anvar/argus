import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  fetchPublishStatus, previewPublish, publishPage,
  type PublishPreview, type PublishResult, type PublishStatus,
} from "../../api";
import { Badge } from "../Badge";
import { expiry } from "../ChangePreview";
import { filled, outlined, small } from "./Table";

/* Publishing the report to Confluence, from the foot of step 7.
 *
 * The page is a fixed sequence of sections, each with a switch. The
 * switches are pre-filled from the team's standing choice and an untick
 * here is for this one publish; the preview renders exactly the sections
 * ticked, so what lands is what was seen. The preview is the rendered
 * block against the block on the page now, as a text diff, and the block
 * itself behind a disclosure - shown as text, never as markup, because
 * the point is to read what will be sent. Changing a switch discards the
 * preview rather than leaving one on screen that no longer describes
 * what the button would do. The publish sends the preview's id and
 * digest, and the server sends the version the preview read, so a page
 * edited in between is refused by Confluence rather than overwritten. */

const box = "rounded-lg px-3 py-2 text-[13px]";

/** The checkboxes as the person has left them; null until touched, so
 *  the standing choice shows through until there is something to keep. */
type Ticks = Record<string, boolean>;

function standing(s: PublishStatus): Ticks {
  return Object.fromEntries(s.sections.map((x) => [x.id, x.on]));
}

function Sections({ status, ticks, onToggle }: { status: PublishStatus; ticks: Ticks; onToggle: (id: string, on: boolean) => void }) {
  return (
    <div className="mb-3">
      <div className="flex flex-wrap gap-x-4 gap-y-1.5">
        {status.sections.map((s) => {
          // The Reason column lives on the per-person table, so without
          // that table there is nowhere for it to go.
          const orphan = s.id === "reasons" && !ticks.people;
          return (
            <label key={s.id} className="flex items-center gap-1.5 text-[13px]" style={{ color: orphan ? "var(--faint)" : "var(--ink)" }}>
              <input
                type="checkbox"
                checked={!!ticks[s.id] && !orphan}
                disabled={orphan}
                onChange={(e) => onToggle(s.id, e.target.checked)}
                className="h-4 w-4 accent-[var(--accent)] disabled:opacity-50"
              />
              {s.label}
            </label>
          );
        })}
      </div>
      <p className="mt-1.5 text-[12px]" style={{ color: "var(--faint)" }}>
        Pre-filled from the team's standing choice. An untick here applies to this publish only.
        Reasons sit on the people table, so they go when it goes.
      </p>
    </div>
  );
}

/** What pressing Publish would do, in a sentence: create or update,
 *  which version, and whether the title moves. The current title is not
 *  in the preview, so a rename is said as where it ends up. */
function Plan({ p }: { p: PublishPreview }) {
  const versions = `version ${p.version} → ${p.version + 1}`;
  return (
    <div className="mb-3 text-[13px]">
      <div className="flex flex-wrap items-center gap-2">
        <Badge tone="info" label={p.action === "create" ? "create" : "update"} />
        <span>
          {p.action === "create"
            ? <>Create <strong>{p.title}</strong></>
            : p.title_changes
              ? <>Update the page, {versions}, and rename it to <strong>{p.title}</strong></>
              : <>Update <strong>{p.title}</strong>, {versions}</>}
        </span>
        {p.url && (
          <a href={p.url} target="_blank" rel="noreferrer" className="lnk text-[12.5px]">Open the page</a>
        )}
      </div>
      {p.how === "migrated" && (
        <p className={`mt-2 ${box}`} style={{ background: "var(--info-bg)", color: "var(--info)" }}>
          The old tool's markers on this page will be replaced by Argus's, so the block stays in one place.
        </p>
      )}
      {p.how === "appended" && (
        <p className={`mt-2 ${box}`} style={{ background: "var(--info-bg)", color: "var(--info)" }}>
          This page has no markers; the block will be added at the end and your prose kept.
        </p>
      )}
      <p className="mt-2 text-[12.5px]" style={{ color: "var(--faint)" }}>
        Preview expires at {expiry(p.expires_at)}.
      </p>
    </div>
  );
}

/** The diff, one line each, with the change said by the sign in the
 *  margin and only then by the colour. */
function Diff({ p }: { p: PublishPreview }) {
  const added = p.diff.filter((l) => l.kind === "add").length;
  const removed = p.diff.filter((l) => l.kind === "del").length;
  return (
    <div>
      <div className="mb-2 flex flex-wrap items-center gap-2">
        {added > 0 && <Badge tone="good" label={`+${added} added`} />}
        {removed > 0 && <Badge tone="critical" label={`−${removed} removed`} />}
        {added === 0 && removed === 0 && <Badge tone="neutral" label="no changes" title="The block on the page already reads like this" />}
      </div>
      <pre
        className="max-h-96 overflow-auto rounded-lg px-3 py-2 font-mono text-[12px] leading-5 whitespace-pre-wrap"
        style={{ background: "var(--surface-2)", color: "var(--ink)" }}
      >
        {p.diff.map((l, i) => (
          <div
            key={i}
            style={{
              color: l.kind === "add" ? "var(--good)" : l.kind === "del" ? "var(--crit)" : "var(--faint)",
              textDecoration: l.kind === "del" ? "line-through" : undefined,
            }}
          >
            <span className="inline-block w-4 select-none">{l.kind === "add" ? "+" : l.kind === "del" ? "−" : " "}</span>
            {l.text}
          </div>
        ))}
      </pre>
      <details className="mt-2">
        <summary className="cursor-pointer text-[12.5px]" style={{ color: "var(--muted)" }}>Show the whole block</summary>
        <pre
          className="mt-2 max-h-96 overflow-auto rounded-lg px-3 py-2 font-mono text-[12px] leading-5 whitespace-pre-wrap"
          style={{ background: "var(--surface-2)", color: "var(--ink)" }}
        >
          {p.block}
        </pre>
      </details>
    </div>
  );
}

function Outcome({ r }: { r: PublishResult }) {
  return (
    <div className="mt-3 flex flex-wrap items-center gap-2 text-[13px]">
      <Badge tone="good" label={r.action === "create" ? "Created" : "Updated"} />
      <span style={{ color: "var(--muted)" }}>version {r.version}</span>
      {r.url && <a href={r.url} target="_blank" rel="noreferrer" className="lnk">Open the page</a>}
    </div>
  );
}

export function PublishStep({ sprintNumber }: { sprintNumber: number }) {
  const qc = useQueryClient();
  const status = useQuery({ queryKey: ["publish-status"], queryFn: fetchPublishStatus, staleTime: 60_000 });
  const [ticks, setTicks] = useState<Ticks | null>(null);
  const [preview, setPreview] = useState<PublishPreview | null>(null);
  const [result, setResult] = useState<PublishResult | null>(null);
  const [previewed, setPreviewed] = useState(false);

  const s = status.data;
  const on: Ticks = ticks ?? (s ? standing(s) : {});
  const selected = (s?.sections ?? []).filter((x) => on[x.id] && !(x.id === "reasons" && !on.people)).map((x) => x.id);

  const ask = useMutation({
    mutationFn: () => previewPublish(sprintNumber, selected),
    onSuccess: (data) => {
      setPreview(data);
      setResult(null);
      setPreviewed(true);
    },
  });
  const publish = useMutation({
    mutationFn: () => publishPage(preview!.id, preview!.digest),
    onSuccess: (r) => {
      setResult(r);
      // The report now knows its page, and the publish left an audit row.
      qc.invalidateQueries({ queryKey: ["sprint-report"] });
      qc.invalidateQueries({ queryKey: ["writes"] });
    },
  });

  const toggle = (id: string, checked: boolean) => {
    setTicks({ ...on, [id]: checked });
    setPreview(null);
    setResult(null);
    ask.reset();
    publish.reset();
  };

  const err = (m: { error: unknown }) => (m.error instanceof Error ? m.error.message : m.error ? String(m.error) : "");
  const expired = preview ? new Date(preview.expires_at).getTime() <= Date.now() : false;
  const allowed = s?.allowed ?? false;
  const canPublish = allowed && !!preview && !result && !publish.isPending && !expired;

  return (
    <section className="mt-8">
      <h3 className="mb-1 text-[11px] font-semibold tracking-wide uppercase" style={{ color: "var(--faint)" }}>
        Publish
      </h3>
      <p className="mb-3 text-[12.5px]" style={{ color: "var(--muted)" }}>
        The sprint's page in Confluence, built from what is saved here. Only the block between
        Argus's markers is written; everything else on the page is kept.
      </p>

      {status.isLoading && <p className="text-[13px]" style={{ color: "var(--muted)" }}>Checking whether publishing is set up…</p>}
      {status.error && (
        <p className={box} style={{ background: "var(--crit-bg)", color: "var(--crit)" }}>{String(status.error)}</p>
      )}

      {s && !s.configured && (
        <div className="rounded-lg px-4 py-4" style={{ background: "var(--surface-2)" }}>
          <div className="flex flex-wrap items-center gap-2">
            <Badge tone="neutral" label="not set up" />
            <span className="text-[13px]">Publishing is not set up for this deployment.</span>
          </div>
          <p className="mt-1.5 text-[12.5px]" style={{ color: "var(--muted)" }}>
            {s.reason ? `${s.reason} ` : ""}
            Set <code className="font-mono text-[12px]">ARGUS_CONFLUENCE_SPACE_ID</code> and{" "}
            <code className="font-mono text-[12px]">ARGUS_CONFLUENCE_PARENT_PAGE_ID</code> to the space and the page
            the sprint pages sit under; a page is then previewed here before anything is written.
          </p>
        </div>
      )}

      {s && s.configured && (
        <>
          <Sections status={s} ticks={on} onToggle={toggle} />

          <div className="mb-3 flex flex-wrap items-center gap-2 text-[12.5px]" style={{ color: "var(--muted)" }}>
            <button onClick={() => ask.mutate()} disabled={ask.isPending || publish.isPending || selected.length === 0} className={small} style={outlined}>
              {previewed ? "Preview again" : "Preview page"}
            </button>
            {selected.length === 0 && <span>Tick at least one section.</span>}
            {ask.isPending && <span>Rendering the page and reading the current one…</span>}
          </div>
          {ask.error && (
            <p className={`mb-3 ${box}`} style={{ background: "var(--crit-bg)", color: "var(--crit)" }}>{err(ask)}</p>
          )}

          {preview && (
            <>
              <Plan p={preview} />
              <Diff p={preview} />

              <div
                className="mt-5 flex flex-wrap items-center justify-between gap-2 rounded-lg px-3 py-2.5"
                style={{ background: "var(--surface-2)" }}
              >
                {!allowed ? (
                  <span className="text-[12.5px]" style={{ color: "var(--muted)" }}>
                    Publishing is off for this deployment: <code className="font-mono text-[12px]">{s.setting}</code> is unset.
                    The preview is all there is until it is set.
                  </span>
                ) : (
                  <span className="text-[12.5px]" style={{ color: expired ? "var(--warn)" : "var(--muted)" }}>
                    {result
                      ? "Published."
                      : expired
                        ? "This preview has expired. Preview again before publishing."
                        : preview.action === "create"
                          ? "Creates the page under the configured parent."
                          : "Writes the block to the page, refusing if the page has been edited since the preview."}
                  </span>
                )}
                {allowed && !result && (
                  <button
                    onClick={() => publish.mutate()}
                    disabled={!canPublish}
                    className="rounded-lg px-3 py-1.5 text-sm font-medium disabled:opacity-40"
                    style={filled}
                  >
                    {publish.isPending ? "Publishing…" : "Publish"}
                  </button>
                )}
              </div>

              {publish.error && (
                <p className={`mt-2 ${box}`} style={{ background: "var(--crit-bg)", color: "var(--crit)" }}>{err(publish)}</p>
              )}
              {result && <Outcome r={result} />}
            </>
          )}
        </>
      )}
    </section>
  );
}
