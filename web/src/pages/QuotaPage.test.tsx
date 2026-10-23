import "@testing-library/jest-dom/vitest"
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { describe, expect, it, vi } from "vitest"
import { QuotaPage } from "@/pages/QuotaPage"

const setBudgetLimits = vi.hoisted(() => vi.fn())
vi.mock("@/hooks/useBudgetLimits", () => ({
  setBudgetLimits,
  useRouterTable: () => ({
    data: {
      path: ".orchestrator/model_router.yaml",
      global_max: 6,
      routes: [{ key: "claude/sonnet", backend: "claude", cli_model: "claude-sonnet-4-6", tier: "premium", max_parallel: 3 }],
    },
  }),
}))

function renderPage() {
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <QuotaPage />
    </QueryClientProvider>,
  )
}

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
    renderPage()
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

  // "Edit limits" sends only what changed, through orch budget set's endpoint,
  // and shows the server's refusal on the row.
  it("edits a provider's limits and shows a refusal", async () => {
    setBudgetLimits.mockResolvedValueOnce({ token_budget: 2000, window_hours: 5, threshold_pct: 90 })
    renderPage()
    fireEvent.click(screen.getByRole("button", { name: "Edit limits" }))
    const row = screen.getByRole("form", { name: "codex" })
    fireEvent.change(within(row).getByRole("spinbutton", { name: "Token budget" }), { target: { value: "2000" } })
    fireEvent.click(within(row).getByRole("button", { name: "Save" }))
    await waitFor(() => expect(setBudgetLimits).toHaveBeenCalledWith("codex", { token_budget: 2000 }))
    expect(await screen.findByText("Saved to budgets.yaml")).toBeInTheDocument()

    setBudgetLimits.mockRejectedValueOnce(new Error("invalid budget limits: threshold_pct must be more than 0 and at most 100"))
    fireEvent.change(within(row).getByRole("spinbutton", { name: "Pauses at (%)" }), { target: { value: "150" } })
    fireEvent.click(within(row).getByRole("button", { name: "Save" }))
    expect(await within(screen.getByRole("dialog")).findByRole("alert")).toHaveTextContent("at most 100")
  })

  it("shows the routing table from model_router.yaml", () => {
    renderPage()
    const table = screen.getByRole("region", { name: "Model routing" })
    expect(within(table).getByText("claude/sonnet")).toBeInTheDocument()
    expect(within(table).getByText("claude-sonnet-4-6")).toBeInTheDocument()
    expect(within(table).getByText("6")).toBeInTheDocument()
  })
})
