import "@testing-library/jest-dom/vitest"
import { fireEvent, render, screen } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { MemoryRouter } from "react-router-dom"
import { describe, expect, it, vi } from "vitest"
import type { MetricsResponse } from "@/hooks/useMetrics"
import { UsagePage } from "@/pages/UsagePage"

const useMetrics = vi.hoisted(() => vi.fn())
vi.mock("@/hooks/useMetrics", () => ({ useMetrics }))
vi.mock("@/hooks/useEventStream", () => ({ useEventStream: () => ({ lastEventAt: null }) }))

const data: MetricsResponse = {
  project_id: "demo", total_cost_usd: 3.49, estimated_cost_usd: 2.5, estimate_hours_total: 0,
  by_model: [{ model: "gpt-5", tasks_total: 1, tokens_in: 300, tokens_out: 100, cost_usd: 2.5 }],
  by_day: [], period: "7d", dispatches: 3, tokens_in: 1500, cache_read_tokens: 800, cache_creation_tokens: 0, tokens_out: 650,
  by_backend_day: [{ date: "2026-09-02", backend: "codex", tokens: 400, dispatches: 1, cost_usd: 2.5 }],
  recent: [{ ts: "2026-09-02T10:00:00Z", task_id: "T-2", backend: "codex", model: "gpt-5", tokens_in: 300, tokens_out: 100, cost_usd: 2.5, estimated: true }],
}

describe("UsagePage", () => {
  // The totals by kind, the cost marked as partly estimated, and the latest
  // dispatches linking to their task.
  it("shows the period's totals and recent dispatches, and changes period", () => {
    useMetrics.mockReturnValue({ data, isLoading: false, isError: false })
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
    expect(screen.getByText("~ partly estimated from pricing.yaml, not actual billing")).toBeInTheDocument()
    expect(screen.getByRole("link", { name: "T-2" })).toHaveAttribute("href", "/work/tasks/T-2")
    fireEvent.click(screen.getByRole("radio", { name: "30D" }))
    expect(useMetrics).toHaveBeenLastCalledWith("30d")
  })
})
