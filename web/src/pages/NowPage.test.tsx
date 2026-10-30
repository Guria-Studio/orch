import "@testing-library/jest-dom/vitest"
import { fireEvent, render, screen, within } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import type { Estimate, Now } from "@/hooks/useNow"
import type { Onboarding, ReceiptPayload } from "@/hooks/useReceipt"
import { setLanguage } from "@/i18n"
import { englishLeaks, visibleText } from "@/i18n/leaks"
import { formatElapsed } from "@/lib/time"
import { NowPage } from "@/pages/NowPage"

const NOW = Date.parse("2026-09-16T12:00:00Z")
const state: { now: Now | undefined; receipt: ReceiptPayload | undefined; onboarding: Onboarding | undefined } = {
  now: undefined,
  receipt: undefined,
  onboarding: undefined,
}
const refetch = vi.fn()

vi.mock("@/hooks/useReceipt", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/hooks/useReceipt")>()),
  useReceipt: () => ({ data: state.receipt }),
  useOnboarding: () => ({ data: state.onboarding, refetch, isFetching: false }),
}))

let estimate: Estimate | undefined
vi.mock("@/hooks/useNow", () => ({
  useNow: () => ({ data: state.now, isLoading: false, isError: false }),
  useEstimate: (enabled: boolean) => ({ data: enabled ? estimate : undefined }),
  useTicker: () => NOW,
}))
vi.mock("@/hooks/useEventStream", () => ({ useEventStream: () => ({ status: "open", lastEventAt: null }) }))
vi.mock("@/hooks/useMilestones", () => ({
  useMilestones: () => ({
    data: [{ phase: 0, name: "Foundation", status: "active", progress: { total: 3, done: 2, pct: 66 }, in_progress: 1, blocked: 0, eta: null }],
  }),
}))
vi.mock("@/hooks/useBudgetSummary", () => ({
  useBudgetSummary: () => ({
    data: {
      available: true,
      preset: "conservative",
      path: "budgets.yaml",
      waiting: [],
      rows: [
        {
          provider: "claude", token_budget: 480_000, tokens_used: 29_394, raw_tokens_used: 60_735, pct: 6,
          threshold_pct: 80, over_threshold: false, window_hours: 5, reset_at: null,
          cost_usd: 1.2, cost_source: "reported", estimated_cost_usd: 0,
        },
      ],
    },
  }),
}))
vi.mock("@/hooks/useTasks", () => ({ useTasks: () => ({ data: { tasks: [{ id: "F3.T1", title: "Cursor pagination" }] } }) }))
const activityCalls: Array<[string | null, boolean]> = []
vi.mock("@/hooks/useTaskActivity", () => ({
  useTaskActivity: (taskId: string | null, live: boolean) => {
    activityCalls.push([taskId, live])
    return { steps: [{ kind: "tool", tool: "Edit", detail: "routes/items.ts" }], exists: true, loaded: true, failed: false }
  },
}))

function base(): Now {
  return {
    project: "acme-billing-api",
    run: { run_id: "run-7", started_at: "2026-09-16T11:00:00Z", updated_at: "", mode: "run", status: "live" },
    working: [
      {
        task_id: "F2.T2", title: "POST /items", phase: 2, provider: "claude", model: "claude/sonnet",
        attempt: 2, max_attempts: 3, started_at: "2026-09-16T11:53:19Z",
      },
    ],
    slots: { used: 1, max: 4 },
    summary: { total: 22, done: 14, in_progress: 1, blocked: 1, backlog: 0 },
    pace: {
      available: true, velocity_per_day: 1, done_count: 14, remaining_tasks: 7, remaining_hours: 9,
      blocked_count: 1, eta_days: 7, eta_date: "2026-09-23", confidence: "high", blockers: [],
    },
    attention: [
      { task_id: "F2.T3", title: "DELETE /items/{id}", phase: 2, reason: "needs a product decision", blocked_at: "2026-09-16T09:00:00Z", estimate_hours: 1 },
    ],
    waiting_for_budget: [{ task_id: "F3.T1", provider: "codex", reset_at: "2026-09-16T14:00:00Z", reason: "blocked-by-budget:codex" }],
  }
}

function renderPage() {
  return render(
    <MemoryRouter>
      <NowPage />
    </MemoryRouter>,
  )
}

describe("NowPage", () => {
  beforeEach(() => {
    state.now = base()
    state.receipt = undefined
    state.onboarding = { complete: true, items: [] }
  })

  it("shows each agent at work with a running clock and its attempt", () => {
    renderPage()
    const working = screen.getByRole("heading", { name: "Working now" }).closest("section")!
    expect(within(working).getByText("1 of 4 slots")).toBeInTheDocument()
    expect(within(working).getByText("POST /items")).toBeInTheDocument()
    // The design gives the model and the attempt their own columns.
    expect(within(working).getByText("claude/sonnet")).toBeInTheDocument()
    expect(within(working).getByText("attempt 2 of 3")).toBeInTheDocument()
    expect(within(working).getByText("6:41")).toBeInTheDocument()
  })

  it("opens a working row onto what its agent is doing, followed live", () => {
    activityCalls.length = 0
    renderPage()
    // The design's eye button opens the activity; the id and title link to the task page.
    expect(screen.getByRole("link", { name: "POST /items" })).toHaveAttribute("href", "/work/tasks/F2.T2")
    const row = screen.getByRole("button", { name: "Show what F2.T2 is doing" })
    expect(row).toHaveAttribute("aria-expanded", "false")
    expect(activityCalls).toHaveLength(0)
    fireEvent.click(row)
    expect(row).toHaveAttribute("aria-expanded", "true")
    expect(screen.getByText("routes/items.ts")).toBeInTheDocument()
    expect(activityCalls[0]).toEqual(["F2.T2", true])
  })

  it("lists blocked and budget-waiting tasks, and unblocking shows the command instead of doing it", () => {
    renderPage()
    const attention = screen.getByRole("heading", { name: "Needs your attention" }).closest("section")!
    expect(within(attention).getByText(/needs a product decision/)).toBeInTheDocument()
    expect(within(attention).getByText("Cursor pagination")).toBeInTheDocument()
    expect(within(attention).getByText(/Waiting for codex's budget window, resets in 2 hours/)).toBeInTheDocument()
    fireEvent.click(within(attention).getByRole("button", { name: "Unblock" }))
    expect(screen.getByText("orch task set --id F2.T3 --status todo")).toBeInTheDocument()
  })

  it("says the ETA leaves blocked work out, and budget shows weighted and raw tokens", () => {
    renderPage()
    // The finish date is its own figure, with what it leaves out beneath it.
    expect(screen.getByText("Estimated finish")).toBeInTheDocument()
    expect(screen.getByText("excludes 1 blocked task")).toBeInTheDocument()
    // The design's budget row: provider, window and weighted tokens (raw and
    // cost source stay on the Budget page).
    expect(screen.getByText("29,394 / 480,000 tokens")).toBeInTheDocument()
    expect(screen.getByText("5h window")).toBeInTheDocument()
  })

  it("says so plainly when nothing runs and nothing needs attention", () => {
    state.now = { ...base(), run: null, working: [], attention: [], waiting_for_budget: [] }
    renderPage()
    expect(screen.getByText(/This project has not run yet/)).toBeInTheDocument()
    expect(screen.getByText(/Nothing is running/)).toBeInTheDocument()
    expect(screen.getByText(/Nothing needs you/)).toBeInTheDocument()
  })

  it("says what the next run would take while nothing runs", () => {
    estimate = {
      tasks: ["A", "B", "C"], cost_usd: 3.2, cost_by_backend: { claude: 3.2 }, wall_s: 7800, critical_path_s: 5400,
      slots: 4, past_attempts: 37, attempts_per_task: 1.3, confidence: "medium",
    }
    state.now = { ...base(), run: null, working: [], attention: [], waiting_for_budget: [] }
    renderPage()
    expect(screen.getByText("Next run: ≈ 3 tasks · ≈ $3.20 · ≈ 2h 10m with 4 slots")).toBeInTheDocument()
    expect(screen.getByText("From 37 past attempts in this project · confidence medium")).toBeInTheDocument()
    // While a run works, the page shows the agents instead.
    state.now = base()
    renderPage()
    expect(screen.getAllByText(/Next run/)).toHaveLength(1)
    estimate = undefined
  })

  it("shows the last finished run's receipt, with costs labelled by where they come from", () => {
    state.receipt = {
      markdown: "## orch run `run-6` — finished\n",
      summary: null,
      built_with: "Built with [orch](https://github.com/Guria-Studio/orch)",
      receipt: {
        run_id: "run-6", started_at: "2026-09-15T08:00:00Z", finished_at: "2026-09-15T10:05:00Z", finished: true,
        wall_seconds: 7500, agent_seconds: 3000, dispatches: 3, failed_attempts: 1, retries: 1,
        done: [{ task_id: "A", title: "Alpha" }, { task_id: "B", title: "Beta" }], blocked: [{ task_id: "C", title: "Gamma" }],
        providers: [
          { provider: "claude", dispatches: 2, tokens_in: 12000, tokens_out: 800, weighted_tokens: 4000, cost_usd: 1.25, estimated_cost_usd: 0, cost_source: "reported" },
          { provider: "codex", dispatches: 1, tokens_in: 3000, tokens_out: 200, weighted_tokens: 3200, cost_usd: 0.4, estimated_cost_usd: 0.4, cost_source: "estimated" },
          { provider: "gemini", dispatches: 1, tokens_in: 0, tokens_out: 0, weighted_tokens: 0, cost_usd: 0, estimated_cost_usd: 0, cost_source: "no_data" },
        ],
        total_cost_usd: 1.65, estimated_cost_usd: 0.4,
        prs: [{ task_id: "A", title: "Alpha", url: "https://github.com/x/y/pull/7", ci_status: "success" }],
      },
    }
    renderPage()
    const last = screen.getByRole("heading", { name: "Last run" }).closest("section")!
    // The design's label/value rows; tokens and cost are the run's totals.
    expect(within(last).getByText("2 tasks done")).toBeInTheDocument()
    expect(within(last).getByText(/1 blocked/)).toBeInTheDocument()
    expect(within(last).getByText("1 failed attempt")).toBeInTheDocument()
    expect(within(last).getByText("2h 5m wall clock · 50m of agents")).toBeInTheDocument()
    expect(within(last).getByText("1 PR, 1 passed CI")).toBeInTheDocument()
    expect(within(last).getByText("7,200 weighted · 16,000 raw")).toBeInTheDocument()
    expect(within(last).getByText("~$1.65")).toBeInTheDocument()
    expect(within(last).getByText("estimated")).toBeInTheDocument()
  })

  it("walks a new project to its first run, with the command for each unmet step", () => {
    state.now = { ...base(), run: null, working: [], attention: [], waiting_for_budget: [] }
    state.onboarding = {
      complete: false,
      items: [
        { id: "providers", done: false, optional: false, detail: "claude not on PATH", command: "orch doctor" },
        { id: "budget", done: true, optional: false, detail: "" },
        { id: "tasks", done: true, optional: false, detail: "" },
        { id: "vcs", done: false, optional: true, detail: "", link: "/delivery/ci" },
        { id: "first_run", done: false, optional: false, detail: "", command: "orch run" },
      ],
    }
    renderPage()
    const card = screen.getByRole("heading", { name: "Get to your first run" }).closest("section")!
    expect(within(card).getByText("2 of 4 required")).toBeInTheDocument()
    expect(within(card).getByText("claude not on PATH")).toBeInTheDocument()
    expect(within(card).getByText("orch doctor")).toBeInTheDocument()
    expect(within(card).getByText("orch run")).toBeInTheDocument()
    expect(within(card).getByText("Needed only for worktrees and automatic PRs.")).toBeInTheDocument()
    // The checklist replaces the bare "has not run yet" line.
    expect(screen.queryByText(/This project has not run yet/)).not.toBeInTheDocument()
    fireEvent.click(within(card).getByRole("button", { name: "Check again" }))
    expect(refetch).toHaveBeenCalled()
  })
})

describe("NowPage in Portuguese", () => {
  afterEach(() => setLanguage("en"))

  // Every section at once — working, attention, budget, onboarding — so an
  // English string left outside the dictionary shows up here.
  it("leaves no English on the page", () => {
    state.now = base()
    state.receipt = undefined
    state.onboarding = {
      complete: false,
      items: [
        { id: "providers", done: false, optional: false, detail: "", command: "orch doctor" },
        { id: "vcs", done: false, optional: true, detail: "", link: "/delivery/ci" },
      ],
    }
    setLanguage("pt")
    const { container } = renderPage()
    expect(screen.getByRole("heading", { name: "Trabalhando agora" })).toBeInTheDocument()
    expect(screen.getByText("29.394 / 480.000 tokens")).toBeInTheDocument()
    expect(englishLeaks(visibleText(container), "pt")).toEqual([])
  })

  it("says the next run's estimate in Portuguese", () => {
    estimate = {
      tasks: ["A"], cost_usd: 0.75, cost_by_backend: { claude: 0.75 }, wall_s: 600, critical_path_s: 600,
      slots: 4, past_attempts: 0, attempts_per_task: 1, confidence: "none",
    }
    state.now = { ...base(), run: null, working: [], attention: [], waiting_for_budget: [] }
    state.onboarding = undefined
    setLanguage("pt")
    const { container } = renderPage()
    expect(screen.getByText(/Próxima execução: ≈ 1 tarefa ·/)).toBeInTheDocument()
    expect(screen.getByText("Ainda sem histórico: é uma estimativa aproximada")).toBeInTheDocument()
    expect(englishLeaks(visibleText(container), "pt")).toEqual([])
    estimate = undefined
  })
})

describe("formatElapsed", () => {
  it("reads like a clock", () => {
    expect(formatElapsed(37_000)).toBe("0:37")
    expect(formatElapsed(401_000)).toBe("6:41")
    expect(formatElapsed(3_729_000)).toBe("1:02:09")
    expect(formatElapsed(-5)).toBe("0:00")
  })
})
