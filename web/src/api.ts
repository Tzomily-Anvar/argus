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
  note?: string;
  registered: boolean;
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
  jql?: string;
};

export type SprintReport = {
  sprint: {
    jira_id: number;
    number: number;
    name: string;
    state: string;
    starts: string;
    ends: string;
    provisional: boolean;
    browse_url: string;
  };
  summary: {
    baseline_total: number;
    capacity_total: number;
    delivered_total: number;
    planned_days_off: number;
    unplanned_days_off: number;
    promised: number;
    injected: number;
    completed: number;
    issue_count: number;
    done_count: number;
    unattributed_points: number;
  };
  people: SprintPerson[];
  stories_concluded: { key: string; summary: string; points: number; epic?: string; url: string }[];
  epics: EpicGroup[];
  carryover: SprintRow[];
  flags: SprintFlag[];
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


export type StoredPerson = {
  account_id: string;
  name: string;
  baseline: number;
  active: boolean;
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

export const savePerson = (p: StoredPerson) => putJSON("/api/sprint/people", p);
export const saveCapacity = (c: StoredCapacity) => putJSON("/api/sprint/capacity", c);


export type Conventions = {
  hours_per_point: number;
  hours_per_day: number;
  sprint_length_days: number;
};

export const fetchConventions = () => getJSON<Conventions>("/api/sprint/conventions");
