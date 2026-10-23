import "@testing-library/jest-dom/vitest"
import { render, screen, within } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"
import { QuotaPage } from "@/pages/QuotaPage"

vi.mock("@/hooks/useQuota", () => ({
  useQuota: () => ({
    isLoading: false,
    data: { providers: [{ backend: "claude", seen_at: new Date().toISOString(), windows: [{ window: "five_hour", utilization: 0.87, resets_at: 0 }, { window: "seven_day", utilization: 0.13, resets_at: 0 }] }] },
  }),
}))
vi.mock("@/hooks/useBudgetSummary", () => ({
  useBudgetSummary: () => ({
    isLoading: false,
    data: {
      available: true, preset: "conservative", path: "", waiting: [],
      rows: [{ provider: "codex", token_budget: 1000, tokens_used: 920, raw_tokens_used: 920, pct: 92, threshold_pct: 90, over_threshold: true, window_hours: 5, reset_at: null, cost_usd: 0, cost_source: "none", estimated_cost_usd: 0 }],
    },
  }),
}))

describe("QuotaPage", () => {
  // claude's own windows where it reported them; orch's budget window where
  // one is configured; and each card says which it lacks.
  it("shows the reported windows and orch's budget window per provider", () => {
    render(<QuotaPage />)
    const claude = screen.getByRole("region", { name: "claude" })
    expect(within(claude).getByRole("progressbar", { name: "5-hour window" })).toHaveAttribute("aria-valuenow", "87")
    expect(within(claude).getByText("No budget window for this provider.")).toBeInTheDocument()
    const codex = screen.getByRole("region", { name: "codex" })
    expect(within(codex).getByText("Paused")).toBeInTheDocument()
    expect(within(codex).getByText(/No dispatch has reported/)).toBeInTheDocument()
  })
})
