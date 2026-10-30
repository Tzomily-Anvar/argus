import { createContext, useContext, useMemo } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { fetchSeen, markSeen, unmarkSeen, type PRRow } from "./api";

/* Whether you have looked at a pull request, on every tab.
 *
 * Two things say so. Your own review, read from GitHub by the sweep,
 * with the commit it was left on; and a mark you set here, kept in
 * Argus and never sent to GitHub, for a pull request you read without
 * leaving a review. Both hold a commit, so both come undone the same
 * way: a push moves the head past it, and the pull request reads as
 * changed since you looked, which is the one worth going back to.
 *
 * Only the open list and your own carry the head commit and your
 * review; the other tabs list the same pull requests from other rules,
 * so they are looked up here by repo#number. */

/** Where a pull request stands with you, as the filter offers it. */
export type Looked = "not yet" | "seen" | "reviewed" | "changed since" | "yours";

type Info = { head?: string; review?: string; current?: boolean };

type LookedState = {
  /** Whether marks can be kept: the server has somewhere to keep them. */
  available: boolean;
  of: (r: PRRow) => Looked;
  /** What you last said, when it is the reason the row is reviewed or changed. */
  review: (r: PRRow) => string;
  /** The commit a mark would hold, if the sweep read one. */
  head: (r: PRRow) => string;
  mark: (r: PRRow, on: boolean) => void;
  busy: boolean;
};

const Ctx = createContext<LookedState | null>(null);
export const LookedProvider = Ctx.Provider;

/** The looked-at state for the pull request tool, or null outside it. */
export function useLooked(): LookedState | null {
  return useContext(Ctx);
}

const keyOf = (r: { repo: string; number: number }) => `${r.repo}#${r.number}`;

/** Builds the state from the rows that carry the head and your review,
 *  and the marks the server keeps. `me` is the signed-in login. */
export function useLookedState(prs: PRRow[], me: string): LookedState {
  const qc = useQueryClient();
  // A server without a store answers 404 here; then there is no mark to
  // offer, and your reviews still show.
  const seen = useQuery({ queryKey: ["seen"], queryFn: fetchSeen, retry: false, staleTime: 30_000 });
  const invalidate = () => qc.invalidateQueries({ queryKey: ["seen"] });
  const put = useMutation({ mutationFn: markSeen, onSuccess: invalidate });
  const drop = useMutation({ mutationFn: unmarkSeen, onSuccess: invalidate });

  const info = useMemo(() => {
    const m = new Map<string, Info>();
    for (const r of prs) {
      if (!r.head_sha) continue;
      m.set(keyOf(r), { head: r.head_sha, review: r.my_review, current: r.my_review_current });
    }
    return m;
  }, [prs]);
  const marks = useMemo(
    () => new Map((seen.data?.seen ?? []).map((s) => [s.key, s.watermark])),
    [seen.data],
  );

  const of = (r: PRRow): Looked => {
    if (me && r.author === me) return "yours";
    const i = info.get(keyOf(r));
    const mark = marks.get(keyOf(r));
    if (i?.review && i.current) return "reviewed";
    if (mark && i?.head && mark === i.head) return "seen";
    if (i?.review || mark) return "changed since";
    return "not yet";
  };

  return {
    available: seen.isSuccess,
    of,
    review: (r) => info.get(keyOf(r))?.review ?? "",
    head: (r) => info.get(keyOf(r))?.head ?? "",
    mark: (r, on) => {
      const head = info.get(keyOf(r))?.head;
      if (on && head) put.mutate([{ key: keyOf(r), head }]);
      if (!on) drop.mutate([keyOf(r)]);
    },
    busy: put.isPending || drop.isPending,
  };
}
