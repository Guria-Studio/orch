import { useQuery } from "@tanstack/react-query"
import { apiClient } from "@/lib/api"

// GET /api/receipt and GET /api/onboarding (internal/dashboard/receiptroutes.go).

export type CostSource = "reported" | "estimated" | "no_data"

export interface ReceiptProvider {
  provider: string
  dispatches: number
  tokens_in: number
  tokens_out: number
  /** What the budget gate counts: cache reads at 10%, writes at 125%. */
  weighted_tokens: number
  cost_usd: number
  estimated_cost_usd: number
  cost_source: CostSource
}

export interface TaskRef {
  task_id: string
  title: string
  /** On a blocked task, why it stopped: verify, failure, ci, not_started or agent. */
  cause?: string
  failure_class?: string
  reason?: string
}

/** `orch report summary`: the run in plain words, in the project's language (#456). */
export interface RunSummary {
  lang: string
  lines: string[]
  /** The next one to three steps, each with the command that does it. */
  actions: { text: string; command?: string }[]
}

export interface Receipt {
  run_id: string
  started_at: string
  finished_at: string
  finished: boolean
  wall_seconds: number
  agent_seconds: number
  dispatches: number
  failed_attempts: number
  retries: number
  done: TaskRef[]
  blocked: TaskRef[]
  providers: ReceiptProvider[]
  total_cost_usd: number
  estimated_cost_usd: number
  prs: { task_id: string; title: string; url: string; ci_status: string }[]
  /** Tasks squash-merged into the base locally (vcs.provider: local). */
  merged_locally?: { task_id: string; title: string; base: string; commit: string }[]
}

export interface ReceiptPayload {
  /** The latest finished run; null before any run has finished. */
  receipt: Receipt | null
  /** `orch report receipt`'s Markdown, without the Built-with footer. */
  markdown: string
  built_with: string
  /** The receipt in plain words; null with the receipt. */
  summary: RunSummary | null
}

/** The Markdown a person copies: the receipt, then the footer unless they unticked it. */
export function receiptMarkdown(payload: ReceiptPayload, builtWith: boolean): string {
  return builtWith ? `${payload.markdown}\n${payload.built_with}\n` : payload.markdown
}

/**
 * The receipt as a PNG to post: GET /api/receipt.svg (the card `orch report
 * receipt --svg` prints) drawn on a canvas at twice its size, so it stays
 * sharp on a high-density screen. No image library: the browser rasterises.
 */
export async function receiptImage(runId: string, builtWith: boolean): Promise<Blob> {
  const params: Record<string, string> = { run: runId }
  if (!builtWith) params.built_with = "0"
  const svg = (await apiClient.get<string>("/api/receipt.svg", { params, responseType: "text" })).data
  const url = URL.createObjectURL(new Blob([svg], { type: "image/svg+xml" }))
  try {
    const img = new Image()
    img.src = url
    await img.decode()
    const canvas = document.createElement("canvas")
    canvas.width = img.naturalWidth * 2
    canvas.height = img.naturalHeight * 2
    const ctx = canvas.getContext("2d")
    if (!ctx) throw new Error("no 2d canvas")
    ctx.drawImage(img, 0, 0, canvas.width, canvas.height)
    return await new Promise<Blob>((resolve, reject) =>
      canvas.toBlob((b) => (b ? resolve(b) : reject(new Error("canvas.toBlob gave nothing"))), "image/png"),
    )
  } finally {
    URL.revokeObjectURL(url)
  }
}

export function useReceipt() {
  return useQuery({
    // Under "tasks" so the event stream's invalidation refreshes it: a run
    // finishing is an event.
    queryKey: ["tasks", "receipt"],
    queryFn: async () => (await apiClient.get<ReceiptPayload>("/api/receipt")).data,
  })
}

export type OnboardingId = "providers" | "claude_permissions" | "budget" | "tasks" | "vcs" | "tunnel" | "first_run"

export interface OnboardingItem {
  id: OnboardingId
  done: boolean
  optional: boolean
  detail: string
  command?: string
  link?: string
}

export interface Onboarding {
  /** A run has finished: the checklist has done its job. */
  complete: boolean
  items: OnboardingItem[]
}

export function useOnboarding() {
  return useQuery({
    // Not under "tasks": its checks run the provider CLIs, so it refreshes on
    // "Check again", not on every event.
    queryKey: ["onboarding"],
    queryFn: async () => (await apiClient.get<Onboarding>("/api/onboarding")).data,
    staleTime: Infinity,
  })
}
