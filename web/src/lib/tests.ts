import { API_BASE_URL, getToken } from "@/lib/api"

/** One E2E run the local verify collected: a task attempt and its counts. */
export interface E2ERun {
  run_id: string
  task_id: string
  attempt: number
  at: string
  passed: number
  failed: number
  flaky: number
  skipped: number
}

export interface TestsResponse {
  /** Newest first. */
  runs: E2ERun[]
}

export type E2EStatus = "passed" | "failed" | "flaky" | "skipped"

export interface E2EAttachment {
  name: string
  content_type: string
  /** Relative to the attempt's directory; "" when it was not kept. */
  path?: string
  /** Where the test left it, relative to the task's worktree. */
  source: string
}

export interface E2ETest {
  title: string
  file: string
  line: number
  project?: string
  status: E2EStatus
  duration_ms: number
  retries: number
  error?: string
  location?: string
  steps?: string[]
  attachments?: E2EAttachment[]
}

/** internal/verify's report.json. */
export interface E2EReport {
  task_id: string
  started_at: string
  duration_ms: number
  app_url?: string
  stats: { passed: number; failed: number; flaky: number; skipped: number }
  tests: E2ETest[]
  errors?: string[]
}

/** A run's identity, as the report and file URLs spell it. */
export const runKey = (r: Pick<E2ERun, "run_id" | "task_id" | "attempt">) =>
  `${encodeURIComponent(r.run_id)}/${encodeURIComponent(r.task_id)}/${r.attempt}`

/**
 * A file a test left, as a URL an <img>, <video> or <a> can load: those send
 * no Authorization header, so the token rides in the query, as a shared link's
 * does.
 */
export function attachmentUrl(run: E2ERun, path: string, download = false): string {
  const params = new URLSearchParams()
  const token = getToken()
  if (token) params.set("token", token)
  if (download) params.set("download", "1")
  const query = params.toString()
  const file = path.split("/").map(encodeURIComponent).join("/")
  return `${API_BASE_URL}/api/tests/${runKey(run)}/files/${file}${query ? `?${query}` : ""}`
}

/** Failed first, then flaky, passed and skipped; the report's order within each. */
export function byAttention(tests: E2ETest[]): E2ETest[] {
  const rank: Record<E2EStatus, number> = { failed: 0, flaky: 1, passed: 2, skipped: 3 }
  return tests
    .map((t, i) => ({ t, i }))
    .sort((a, b) => rank[a.t.status] - rank[b.t.status] || a.i - b.i)
    .map(({ t }) => t)
}

export const attachment = (t: E2ETest, name: string) => t.attachments?.find((a) => a.name === name && a.path)
