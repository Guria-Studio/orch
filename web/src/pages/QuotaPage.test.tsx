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
  // One card per provider, as in the design: claude's own windows labelled
  // "reported"; codex, which only has orch's budget window, "estimated" with
  // its threshold.
  it("shows the reported windows, or orch's budget window, per provider", () => {
    render(<QuotaPage />)
    const claude = screen.getByRole("article", { name: "claude" })
    expect(within(claude).getByText("reported")).toBeInTheDocument()
    expect(within(claude).getByRole("progressbar", { name: "Session (5h)" })).toHaveAttribute("aria-valuenow", "87")
    expect(within(claude).getByRole("progressbar", { name: "Weekly (7d)" })).toHaveAttribute("aria-valuenow", "13")
    const codex = screen.getByRole("article", { name: "codex" })
    expect(within(codex).getByText("estimated")).toBeInTheDocument()
    expect(within(codex).getByRole("progressbar", { name: "5h window" })).toHaveAttribute("aria-valuenow", "92")
    expect(within(codex).getByText(/Pauses at 90%/)).toBeInTheDocument()
    expect(screen.getByText("Preset conservative · budgets.yaml")).toBeInTheDocument()
  })
})
