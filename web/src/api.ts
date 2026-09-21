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
