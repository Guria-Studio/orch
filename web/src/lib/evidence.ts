/** One check the local verify ran. */
export interface EvidenceCheck {
  command: string
  duration_s: number
  /** tests, typecheck, lint, build, install, e2e; absent when the command does not say. */
  kind?: string
}

export interface EvidenceReview {
  reviewer?: string
  model?: string
  blocking: number
  findings: { severity: string; file?: string; line?: number; title: string }[]
  /** Why a review that was asked for did not happen. */
  unavailable?: string
}

export interface EvidenceMedia {
  test: string
  kind: "video" | "screenshot" | "trace" | "other"
  /** Relative to e2e.dir. */
  path: string
  bytes: number
}

/** An E2E test tagged @evidence: its steps, in the client's language, and its video. */
export interface Walkthrough {
  title: string
  status: string
  steps?: string[]
  video?: string
  screenshot?: string
}

/** internal/evidence's manifest: what the local verify proved about the pushed attempt (#520). */
export interface Evidence {
  version: number
  task_id: string
  title: string
  run_id: string
  attempt: number
  head_sha?: string
  base_sha?: string
  verified_at: string
  verdict: "passed" | "failed" | "review_unavailable"
  checks: EvidenceCheck[]
  /** verify.app.probes: what the app served, path by path. */
  probes?: { path: string; status: number; ok: boolean; detail?: string }[]
  review?: EvidenceReview
  diff?: { files: number; insertions: number; deletions: number; top?: string[] }
  e2e?: {
    passed: number
    failed: number
    flaky: number
    skipped: number
    /** The attempt's E2E directory under the state dir: e2e/<run>/<task>/attempt-<n>. */
    dir?: string
    media?: EvidenceMedia[]
    walkthroughs?: Walkthrough[]
  }
}
