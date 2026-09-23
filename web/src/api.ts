// The API surface, mirrored from the Go server.

export type RuleResult = {
  rule_id: string;
  data?: unknown;
  error?: string;
  finished_at: string;
  duration_ms: number;
};

export type Snapshot = {
  org: string;
  user: string;
  teams: string[];
  results: Record<string, RuleResult>;
  swept_at: string;
  age_seconds: number;
  sweeping: boolean;
  error?: string;
  ready: boolean;
};

export type RuleParam = {
  name: string;
  desc: string;
  default: unknown;
  value: unknown;
  env: string;
};

export type RuleInfo = {
  id: string;
  title: string;
  description: string;
  why: string;
  enabled: boolean;
  enabled_env: string;
  params: RuleParam[];
};

export type PRRow = {
  repo: string;
  number: number;
  title: string;
  author: string;
  created: string;
  updated: string;
  age_days: number;
  url: string;
  labels?: string[];
  draft?: boolean;
  review_decision?: string;
  has_qa_label?: boolean;
  qa_label_used?: boolean;
  real_failures?: string[];
  policy_failures?: string[];
  checks_green?: boolean;
};

export type PolicyHint = {
  name: string;
  token: string;
  failing_on: number;
  out_of: number;
  env: string;
  suggestion: string;
};

export type MergeReadiness = {
  rows: PRRow[];
  policy_hints: PolicyHint[] | null;
};

export type BranchRow = {
  repo: string;
  branch: string;
  last_commit: string;
  author: string;
  mine: boolean;
  age_days: number;
  url: string;
};

async function getJSON<T>(path: string): Promise<T> {
  const res = await fetch(path, { headers: { Accept: "application/json" } });
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
  return res.json() as Promise<T>;
}

export const fetchSnapshot = () => getJSON<Snapshot>("/api/snapshot");
export const fetchRules = () => getJSON<{ rules: RuleInfo[] }>("/api/rules");

export async function requestRefresh(): Promise<void> {
  const res = await fetch("/api/refresh", { method: "POST" });
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
}


// ---- sprint report ---------------------------------------------------

export type SprintOption = {
  jira_id: number;
  number: number;
  name: string;
  state: string;
  starts?: string;
  ends?: string;
  current: boolean;
  has_report: boolean;
  report_at?: string;
};

/** How an issue stands relative to the sprint being reported on. This is
 *  not the same question as `done`: an issue can be done now and have
 *  been open throughout the sprint you are looking at. */
export type SprintIssueState = "concluded" | "carried" | "finished_earlier";

export type SprintRow = {
  key: string;
  summary: string;
  type: string;
  status: string;
  assignee?: string;
  points: number;
  has_points: boolean;
  done: boolean;
  epic?: string;
  epic_key?: string;
  state: SprintIssueState;
  /** What this sprint counts for the issue, which is not its size: a
   *  ticket finishing here hands back what earlier sprints were already
   *  credited for it, and one still open earns only what was worked on
   *  here. Across every sprint it touched these sum to `points`. */
  credited: number;
  prior_points?: number;
  /** The figure came from the planning estimate because the actual was
   *  never filled in. */
  used_estimate?: boolean;
  /** What refinement agreed the work would cost. Kept beside `points`
   *  rather than folded into it: `used_estimate` above marks the one
   *  case where `points` was read off this same field. */
  estimate?: number;
  has_estimate?: boolean;
  concluded_at?: string;
  /** Whether anybody picked it up during this sprint. An open ticket
   *  nobody touched is not work that happened. */
  active: boolean;
  started_earlier?: boolean;
  time_logged: boolean;
  hours_logged?: number;
  url: string;
};

export type SprintPerson = {
  account_id: string;
  name: string;
  baseline: number;
  capacity: number;
  delivered: number;
  delta: number;
  planned_days_off: number;
  unplanned_days_off: number;
  /** The part of a negative delta that unplanned absence accounts for.
   *  Capacity is reduced by planned leave only, so unplanned absence
   *  shows up here as part of what a shortfall is made of rather than
   *  quietly shrinking the number the shortfall is measured against. */
  shortfall_from_absence: number;
  note?: string;
  /** False for somebody who delivered work here without being on the
   *  team's roster - a contractor, or another team's engineer. */
  on_roster: boolean;
  /** True only for somebody on the roster, opted in, and with a baseline.
   *  Baseline, capacity and delta mean nothing when it is false, and that
   *  person counts towards no capacity total. Their delivered points
   *  still count towards the sprint. */
  measured: boolean;
  rows: SprintRow[];
};

export type EpicGroup = {
  key?: string;
  name: string;
  class?: string;
  points: number;
  share: number;
  issue_count: number;
  /** Absent for work that belongs to no epic, and when no Jira base URL
   *  is configured: there is then nothing to open. */
  url?: string;
};

export type SprintFlag = {
  kind: string;
  message: string;
  key?: string;
  url?: string;
  /** The tickets behind a flag that speaks for several at once, so one
   *  row can stand in for what would otherwise be nine. */
  keys?: string[];
  jql?: string;
};

/** Whether a person has been through this sprint's availability.
 *
 *  Three states, because an empty capacity table means two different
 *  things: nobody has opened this sprint, or somebody opened it and
 *  everyone was at their baseline. The second is an answer. */
export type CapacityReview = {
  state: "not_reviewed" | "adjusted" | "no_adjustments";
  reviewed_at?: string;
  /** How many people were away for any part of the sprint. */
  adjusted: number;
};

/** One ticket whose actual and estimate disagreed. */
export type Divergence = {
  key: string;
  summary: string;
  url: string;
  estimated: number;
  actual: number;
  /** Actual less estimated, sign kept: costing more and costing less are
   *  different situations and are never folded into one magnitude. */
  difference: number;
};

/** What refinement expected the work to cost against what it cost.
 *
 *  Two fields are recorded on a ticket: an estimate agreed at refinement
 *  and the actual filled in when the work finishes. The distance between
 *  them is a reading on how well refinement is calibrating - a
 *  retrospective conversation rather than a score, so nothing here is
 *  named accuracy and nothing here is coloured as a failure.
 *
 *  The estimate field also stands in for a missing actual, flagged as
 *  `estimate_used`. That is a data gap rather than a comparison, and a
 *  ticket sized that way is excluded from every figure below: comparing
 *  the estimate against a copy of itself would score a perfect match out
 *  of a number nobody recorded. */
export type Calibration = {
  /** False when this site cannot make the comparison at all - no estimate
   *  field, or one field doing both jobs. */
  configured: boolean;
  /** Totalled over the comparable tickets only, so the two are readings
   *  of the same subset rather than totals of different things. */
  estimated: number;
  actual: number;
  /** Actual less estimated. Positive means the work cost more than
   *  refinement expected. */
  difference: number;
  /** The difference as a share of the estimate, so 0.3 is a third more
   *  than expected. Absent when nothing estimable was compared. */
  variance: number;
  has_variance: boolean;
  /** The coverage: how many finished tickets carried both figures, out of
   *  how many finished at all. The totals speak for the subset. */
  compared: number;
  finished: number;
  /** Most comparable tickets land on `matched`, so the variance is
   *  usually the work of the handful in `diverged`. */
  matched: number;
  over: number;
  under: number;
  /** Too few tickets to read as anything but a hint. Never a reason to
   *  hide the figure, only to say how much weight it carries. */
  thin: boolean;
  diverged?: Divergence[];
};

/** One sprint's place in the run of them. Carries no tickets: the sprint
 *  report names its own. */
export type CalibrationSprint = {
  number: number;
  name: string;
  calibration: Calibration;
};

/** The run of sprints, oldest first. One sprint's variance is a number;
 *  five in a row is what shows whether refinement is settling. */
export type CalibrationTrend = {
  sprints: CalibrationSprint[];
  /** Some of the run has not been swept yet, so the answer is partial and
   *  worth asking for again. */
  building: boolean;
};

export type SprintReport = {
  sprint: {
    jira_id: number;
    number: number;
    name: string;
    state: string;
    starts: string;
    ends: string;
    /** The stretch this sprint claims work in. Not `starts` and `ends`: a
     *  sprint is completed when somebody clicks Complete Sprint, often
     *  days after the date it was meant to end, and that click decides
     *  which sprint a finished ticket belongs to. */
    counts_from: string;
    counts_until: string;
    provisional: boolean;
    browse_url: string;
  };
  summary: {
    baseline_total: number;
    capacity_total: number;
    delivered_total: number;
    planned_days_off: number;
    unplanned_days_off: number;
    shortfall_from_absence: number;
    promised: number;
    injected: number;
    completed: number;
    issue_count: number;
    done_count: number;
    carried_over: number;
    carried_active: number;
    never_started: number;
    finished_from_earlier: number;
    finished_earlier: number;
    prior_points_deducted: number;
    unattributed_points: number;
  };
  people: SprintPerson[];
  stories_concluded: {
    key: string;
    summary: string;
    points: number;
    epic?: string;
    url: string;
    linked_points: number;
    linked_count: number;
    concluded_at: string;
  }[];
  epics: EpicGroup[];
  carryover: SprintRow[];
  flags: SprintFlag[];
  capacity_review: CapacityReview;
  calibration: Calibration;
  generated_at: string;
};

export type ReportResult = {
  report: SprintReport;
  built_at: string;
  building: boolean;
  stale: boolean;
};

export type Tool = { id: string; label: string; available: boolean };

export const fetchTools = () => getJSON<{ tools: Tool[] }>("/api/tools");
export const fetchSprints = (all = false) =>
  getJSON<{ sprints: SprintOption[] }>(`/api/sprint/sprints${all ? "?all=1" : ""}`);
export const fetchSprintReport = (n: number, refresh = false) =>
  getJSON<ReportResult>(`/api/sprint/report?sprint=${n}${refresh ? "&refresh=1" : ""}`);
/* Its own request rather than part of the report: it costs a Jira sweep
 * per sprint it does not already hold, and the report must not get slower
 * for a section further down the page. */
export const fetchCalibrationTrend = (n: number) =>
  getJSON<CalibrationTrend>(`/api/sprint/calibration?sprint=${n}`);


/** One person on the roster, as stored.
 *
 *  `active` is the opt-in: true means this person is measured against
 *  their baseline, false means they are known and deliberately not
 *  measured - a manager, somebody on loan, somebody who has left. It is
 *  never a reason to delete them, because what they delivered still
 *  happened and an old sprint still has to be able to name them. */
export type StoredPerson = {
  account_id: string;
  name: string;
  baseline: number;
  active: boolean;
  updated_at?: string;
};

export type StoredCapacity = {
  sprint_jira_id: number;
  account_id: string;
  planned_days_off: number;
  unplanned_days_off: number;
  reviewed: boolean;
  note?: string;
};

async function putJSON(path: string, body: unknown): Promise<void> {
  const res = await fetch(path, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (!res.ok) {
    const detail = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(detail.error ?? res.statusText);
  }
}

export const fetchPeople = () => getJSON<{ people: StoredPerson[] }>("/api/sprint/people");
export const savePerson = (p: StoredPerson) => putJSON("/api/sprint/people", p);


// ---- importing the roster from an Atlassian team ---------------------

/** What an import found about one person.
 *
 *  `new` is on the Atlassian team and not on the roster - the thing an
 *  import is for. `on_roster` is on both. `departed` is on the roster and
 *  no longer on the team: raised for a decision, never removed here. */
export type TeamCandidate = {
  account_id: string;
  name: string;
  state: "new" | "on_roster" | "departed";
  baseline: number;
  opted_in: boolean;
  /** The Atlassian account's own state, which is not the same thing as
   *  being opted in here. */
  account_active: boolean;
  /** False for an app or a service desk account. */
  human: boolean;
  /** Whether to tick this one for you. Only a new, active, human account
   *  is; everything else is a decision to make deliberately. */
  suggested: boolean;
};

export type TeamImport = {
  /** False when no Atlassian team is configured, which is an ordinary
   *  answer rather than a failure: the panel explains what to set instead
   *  of offering a button that cannot work. */
  configured: boolean;
  reason?: string;
  team_name?: string;
  candidates: TeamCandidate[];
};

export const fetchTeamImport = () => getJSON<TeamImport>("/api/sprint/team");
export const saveCapacity = (c: StoredCapacity) => putJSON("/api/sprint/capacity", c);

/** Record, or withdraw, the statement that a sprint's availability has
 *  been checked. Recording it with no capacity rows is how "everyone was
 *  available" is said. */
export const saveSprintReview = (sprintJiraID: number, reviewed: boolean) =>
  putJSON("/api/sprint/review", { sprint_jira_id: sprintJiraID, reviewed });


export type Conventions = {
  hours_per_point: number;
  hours_per_day: number;
  sprint_length_days: number;
  /** How a day off is priced: "point" takes a whole point, "share" takes
   *  the baseline's share of one sprint day. */
  absence_cost: "point" | "share";
};

export const fetchConventions = () => getJSON<Conventions>("/api/sprint/conventions");


// ---- writing back --------------------------------------------------------

/** The operations a change set can carry, in the vocabulary the server's
 *  permitted list uses, so one word names a change from the input where
 *  it was typed through to the audit row it will one day leave. */
export type ChangeOp = "points.set" | "assignee.set" | "worklog.add" | "worklog.update" | "worklog.delete";

/** One thing a person asked for: a value typed against a key. Which
 *  fields matter depends on `op`; the rest are ignored by the server. */
export type ChangeRequest = {
  key: string;
  op: ChangeOp;
  points?: number;
  /** An account id on the roster. */
  assignee?: string;
  /** The worklog operations. Hours rather than seconds, because hours is
   *  what the person remembers; the server converts once. */
  person?: string;
  hours?: number;
  started?: string;
  note?: string;
  worklog_id?: string;
};

/** One existing worklog entry as it stood at preview time, reduced to
 *  what decides whether it has changed since. */
export type WorklogGuard = {
  id: string;
  updated: string;
  seconds: number;
  mentions: string[];
};

export type Guard = {
  issue_id: string;
  updated: string;
  /** What the field held at preview time; null means empty. */
  was: unknown;
  status: string;
  entry?: WorklogGuard;
};

/** One operation on one issue, as the server proposes it. `after` is the
 *  shape that will be written; `after_label` is how it reads. */
export type Change = {
  key: string;
  url: string;
  summary: string;
  type: string;
  status: string;
  op: ChangeOp;
  field: string;
  after: unknown;
  after_label: string;
  reason: string;
  person?: string;
  person_label?: string;
  hours?: number;
  started?: string;
  worklog_id?: string;
  guard: Guard;
};

/** A request that produced no change, and why, in words the person who
 *  typed it can act on. */
export type Skip = {
  key: string;
  op: ChangeOp;
  person?: string;
  reason: string;
};

export type ChangeSet = {
  id: string;
  digest: string;
  sprint_jira_id: number;
  built_at: string;
  expires_at: string;
  report_built: string;
  /** The configured Jira account: every edit will appear in Jira as
   *  this person, whoever the change names. */
  actor: string;
  changes: Change[];
  skipped: Skip[];
};

export type WritesStatus = {
  allowed: boolean;
  setting: string;
  reason: string;
};

/** One worklog entry as it is logged now. `people` is who the comment
 *  credits; empty means the entry is its author's own time. */
export type WorklogView = {
  id: string;
  author: string;
  author_label: string;
  people: { id: string; label: string }[];
  hours: number;
  started: string;
  note: string;
  /** Where the entry falls against the sprint being looked at. Only
   *  "inside" credits this sprint. Absent when no sprint was given. */
  window?: "before" | "inside" | "after";
};

/* A POST that reads the server's reason on failure, so a 409 ("open the
 * report first") reaches the screen as its sentence rather than as a
 * status code. The preview is a POST that writes nothing to Jira: the
 * typed values travel in the body because a dozen estimates and a note
 * do not belong in a query string. */
async function postJSON<T>(path: string, body: unknown): Promise<T> {
  const res = await fetch(path, {
    method: "POST",
    headers: { "Content-Type": "application/json", Accept: "application/json" },
    body: JSON.stringify(body),
  });
  if (!res.ok) {
    const detail = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(detail.error ?? res.statusText);
  }
  return res.json() as Promise<T>;
}

export const proposeChanges = (sprint: number, changes: ChangeRequest[]) =>
  postJSON<ChangeSet>("/api/sprint/changes", { sprint, changes });
export const fetchChangeSet = (id: string) =>
  getJSON<ChangeSet>(`/api/sprint/changes/${encodeURIComponent(id)}`);
export const fetchWritesStatus = () => getJSON<WritesStatus>("/api/sprint/writes/status");
export const fetchWorklog = (sprint: number, key: string) =>
  getJSON<{ entries: WorklogView[] }>(`/api/sprint/worklog?sprint=${sprint}&key=${encodeURIComponent(key)}`);


// ---- the close-out -------------------------------------------------------

/** A finished Task or Bug with no points. `suggested` is the refinement
 *  estimate where one exists, and is the value the step pre-fills. */
export type CloseoutSizeRow = {
  key: string;
  summary: string;
  type: string;
  status: string;
  url: string;
  estimate: number | null;
  suggested: number | null;
};

/** Finished with nobody assigned. The suggestion is whoever moved it to
 *  Done, read from the changelog; empty when nobody recognisable did. */
export type CloseoutAssignRow = {
  key: string;
  summary: string;
  type: string;
  status: string;
  url: string;
  suggested_account_id: string;
  suggested_label: string;
  candidates: { account_id: string; label: string }[];
};

/** Open at the close and worked on, with what is logged against it. The
 *  assignee is the suggested person; the hours are always left blank,
 *  because nobody but the person knows them. */
export type CloseoutEffortRow = {
  key: string;
  summary: string;
  status: string;
  /** Where the ticket stood when the sprint closed, which is why it is
   *  in the list; `status` is where it stands now. */
  status_at_close: string;
  url: string;
  assignee_account_id: string;
  assignee_label: string;
  hours_logged: number;
  entries: WorklogView[];
};

/** A Story whose linked work is all done. `suggested` is the sum of that
 *  work and is null when some of it is unsized, in which case `sum_known`
 *  is false and there is nothing to write. */
export type CloseoutStoryRow = {
  key: string;
  summary: string;
  url: string;
  own_points: number | null;
  linked_count: number;
  linked_points: number;
  sum_known: boolean;
  suggested: number | null;
  /** Wrapped up in this sprint. False for an earlier Story listed only
   *  because its points still disagree with the sum beneath it. */
  concluded_here: boolean;
};

export type CloseoutPerson = {
  account_id: string;
  name: string;
  note: string;
};

/** Every table the close-out walks, built from the sprint's report. The
 *  server answers 409 until that report has been opened once. */
export type CloseoutModel = {
  sprint_jira_id: number;
  sprint: number;
  size: CloseoutSizeRow[];
  assign: CloseoutAssignRow[];
  effort: CloseoutEffortRow[];
  stories: CloseoutStoryRow[];
  people: CloseoutPerson[];
};

/** The queued requests for one sprint, as the store holds them. */
export type Draft = {
  sprint_jira_id: number;
  requests: ChangeRequest[];
  updated_at?: string;
};

/** One attempted write, in preview order. `skipped` is the guard
 *  refusing because Jira had moved on; `failed` is Jira refusing. */
export type RowResult = {
  key: string;
  op: ChangeOp;
  outcome: "applied" | "skipped" | "failed";
  reason: string;
  person_label: string;
  after_label: string;
};

export type ApplyResult = {
  rows: RowResult[];
  applied: number;
  skipped: number;
  failed: number;
  /** Why the run stopped before the end, if it did. Empty otherwise. */
  stopped: string;
};

/** One row of the audit log: an attempt to write, whatever came of it. */
export type WriteRecord = {
  id: number | string;
  at: string;
  operation: string;
  target: string;
  before: unknown;
  after: unknown;
  actor: string;
  note: string;
  change_set: string;
  outcome: string;
};

async function deleteJSON(path: string): Promise<void> {
  const res = await fetch(path, { method: "DELETE", headers: { Accept: "application/json" } });
  if (!res.ok) {
    const detail = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(detail.error ?? res.statusText);
  }
}

/* The closeout is asked for by sprint number, like the report it is
 * built from; the draft by Jira id, like the capacity rows it sits
 * beside in the store. */
export const fetchCloseout = (sprint: number) =>
  getJSON<CloseoutModel>(`/api/sprint/closeout?sprint=${sprint}`);
export const fetchDraft = (sprintJiraID: number) =>
  getJSON<Draft>(`/api/sprint/draft?sprint=${sprintJiraID}`);
export const saveDraft = (d: Draft) => putJSON("/api/sprint/draft", d);
export const deleteDraft = (sprintJiraID: number) =>
  deleteJSON(`/api/sprint/draft?sprint=${sprintJiraID}`);

/** The digest travels with the apply so a change set edited under the
 *  viewer's feet is refused rather than written as it now reads. */
export const applyChanges = (id: string, digest: string) =>
  postJSON<ApplyResult>(`/api/sprint/changes/${encodeURIComponent(id)}/apply`, { digest });
/** A new preview that undoes what a batch wrote, built from its audit
 *  rows. Applied like any other. */
export const reverseChanges = (changeSet: string) =>
  postJSON<ChangeSet>("/api/sprint/changes/reverse", { change_set: changeSet });
export const fetchWrites = (limit = 100) =>
  getJSON<{ writes: WriteRecord[] }>(`/api/sprint/writes?limit=${limit}`);
