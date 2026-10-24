import "@testing-library/jest-dom/vitest"
import { fireEvent, render, screen } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { MemoryRouter } from "react-router-dom"
import { describe, expect, it, vi } from "vitest"
import type { MetricsResponse } from "@/hooks/useMetrics"
import type { RouterSuggestResponse } from "@/hooks/useRouterSuggest"
import { UsagePage } from "@/pages/UsagePage"

const useMetrics = vi.hoisted(() => vi.fn())
vi.mock("@/hooks/useMetrics", () => ({ useMetrics }))
const useRouterSuggest = vi.hoisted(() => vi.fn())
vi.mock("@/hooks/useRouterSuggest", () => ({ useRouterSuggest }))
vi.mock("@/hooks/useEventStream", () => ({ useEventStream: () => ({ lastEventAt: null }) }))

const data: MetricsResponse = {
  project_id: "demo", total_cost_usd: 3.49, estimated_cost_usd: 2.5, estimate_hours_total: 0,
  by_model: [{ model: "gpt-5", tasks_total: 1, tokens_in: 300, tokens_out: 100, cost_usd: 2.5 }],
  by_day: [], period: "7d", dispatches: 3, tokens_in: 1500, cache_read_tokens: 800, cache_creation_tokens: 0, tokens_out: 650,
  by_backend_day: [{ date: "2026-09-02", backend: "codex", tokens: 400, dispatches: 1, cost_usd: 2.5 }],
  recent: [{ ts: "2026-09-02T10:00:00Z", task_id: "T-2", backend: "codex", model: "gpt-5", tokens_in: 300, tokens_out: 100, cost_usd: 2.5, estimated: true }],
}

const suggest: RouterSuggestResponse = {
  period: "7d", min_sample: 5,
  kinds: [{
    kind: "work", current: "codex/gpt-5",
    routes: [
      { route: "codex/gpt-5", current: true, tasks: 25, first_try_rate: 0.88, mean_attempts: 1.12, attempts: 28, mean_cost_usd: 1, mean_duration_s: 60 },
      { route: "claude/claude-sonnet", current: false, tasks: 10, first_try_rate: 0.9, mean_attempts: 1.1, attempts: 11, mean_cost_usd: 0.5, mean_duration_s: 60 },
    ],
    suggestion: { key: "work", from: "codex/gpt-5", to: "claude/claude-sonnet", why: "cheaper", to_backend: "claude", to_cli_model: "claude-sonnet", reason: "first-try success as good or within 5 points (90% vs 88%) at 50% of the cost", cost_share: 0.5, first_try_from: 0.88, first_try_to: 0.9 },
  }],
}

describe("UsagePage", () => {
  // The totals by kind, the cost marked as partly estimated, and the latest
  // dispatches linking to their task.
  it("shows the period's totals and recent dispatches, and changes period", () => {
    useMetrics.mockReturnValue({ data, isLoading: false, isError: false })
    useRouterSuggest.mockReturnValue({ data: { period: "7d", min_sample: 5, kinds: [] } })
    render(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter>
          <UsagePage />
        </MemoryRouter>
      </QueryClientProvider>,
    )
    expect(useMetrics).toHaveBeenLastCalledWith("7d")
    expect(screen.getByText("1,500")).toBeInTheDocument()
    expect(screen.getByText("~$3.49")).toBeInTheDocument()
    expect(screen.getByText("Part estimated with pricing.yaml")).toBeInTheDocument()
    // Each recent dispatch is one row-wide link to its task, as in the design.
    expect(screen.getByRole("link", { name: /^T-2/ })).toHaveAttribute("href", "/work/tasks/T-2")
    fireEvent.click(screen.getByRole("radio", { name: "30D" }))
    expect(useMetrics).toHaveBeenLastCalledWith("30d")
  })

  // #457: per route key, each route's record and the suggested change.
  it("shows the routes table with its suggestion", () => {
    useMetrics.mockReturnValue({ data, isLoading: false, isError: false })
    useRouterSuggest.mockReturnValue({ data: suggest })
    render(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter>
          <UsagePage />
        </MemoryRouter>
      </QueryClientProvider>,
    )
    expect(useRouterSuggest).toHaveBeenLastCalledWith("7d")
    expect(screen.getByText("Routes by task kind")).toBeInTheDocument()
    expect(screen.getByText("88%")).toBeInTheDocument()
    expect(screen.getByText(/Suggested: claude\/claude-sonnet/)).toBeInTheDocument()
    expect(screen.getByText("first-try success as good or within 5 points (90% vs 88%) at 50% of the cost.")).toBeInTheDocument()
    expect(screen.getByText("orch router suggest --apply")).toBeInTheDocument()
  })
})
