import "@testing-library/jest-dom/vitest"
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { describe, expect, it, vi } from "vitest"
import { QuotaPage } from "@/pages/QuotaPage"

const setProvider = vi.hoisted(() => vi.fn())
vi.mock("@/lib/api", async (importOriginal) => ({ ...(await importOriginal<typeof import("@/lib/api")>()), setProvider }))
vi.mock("@/hooks/useBudgetLimits", () => ({ setBudgetLimits: vi.fn(), useRouterTable: () => ({ data: undefined }) }))
vi.mock("@/hooks/useQuota", () => ({
  useQuota: () => ({
    isLoading: false,
    data: {
      providers: [
        { backend: "claude", seen_at: new Date().toISOString(), windows: [{ window: "five_hour", utilization: 0.4, resets_at: 0 }] },
        { backend: "codex", seen_at: "", windows: [], disabled: true, reason: "quota spent" },
      ],
    },
  }),
}))
vi.mock("@/hooks/useBudgetSummary", () => ({ useBudgetSummary: () => ({ isLoading: false, data: { available: false, rows: [] } }) }))

function renderPage() {
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <QuotaPage />
    </QueryClientProvider>,
  )
}

describe("QuotaPage provider switch", () => {
  // The design's switch is `orch provider disable|enable`: it says which
  // providers are off, and flipping one calls the API.
  it("shows which providers are off and flips one through the API", async () => {
    setProvider.mockResolvedValue({ backend: "claude", disabled: true })
    renderPage()
    const codex = screen.getByRole("article", { name: "codex" })
    expect(within(codex).getByRole("switch", { name: "Dispatch to codex" })).toHaveAttribute("aria-checked", "false")
    expect(within(codex).getByText(/quota spent/)).toBeInTheDocument()

    const claude = screen.getByRole("article", { name: "claude" })
    fireEvent.click(within(claude).getByRole("switch", { name: "Dispatch to claude" }))
    await waitFor(() => expect(setProvider).toHaveBeenCalledWith("claude", true))
  })

  it("shows the server's reason when it refuses", async () => {
    setProvider.mockRejectedValue(new Error("loopback only"))
    renderPage()
    fireEvent.click(screen.getByRole("switch", { name: "Dispatch to claude" }))
    expect(await screen.findByRole("alert")).toHaveTextContent("loopback only")
  })
})
