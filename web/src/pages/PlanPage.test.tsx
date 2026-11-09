import "@testing-library/jest-dom/vitest"
import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { MemoryRouter } from "react-router-dom"
import { afterEach, describe, expect, it, vi } from "vitest"
import type { FlowState } from "@/hooks/useFlow"
import { setLanguage } from "@/i18n"
import { englishLeaks, visibleText } from "@/i18n/leaks"
import { PlanPage } from "@/pages/PlanPage"

const get = vi.fn()
const post = vi.fn()
vi.mock("@/lib/api", () => ({ apiClient: { get: (...a: unknown[]) => get(...a), post: (...a: unknown[]) => post(...a) } }))

const base: FlowState = {
  clis: [
    { name: "claude", installed: true },
    { name: "codex", installed: true },
  ],
  cwd: "/work/app",
  root: "/work/app",
  log: [],
  tasks: [],
  planning: false,
  steps: [],
  applied: false,
  ready: 0,
  running: false,
}

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <PlanPage />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

// #548: the project's own Plan page, the welcome page's plan and apply steps.
describe("PlanPage", () => {
  afterEach(() => {
    vi.clearAllMocks()
    setLanguage("en")
  })

  it("plans the idea through /api/flow", async () => {
    get.mockResolvedValue({ data: base })
    post.mockResolvedValue({ data: { ...base, planning: true } })
    renderPage()
    fireEvent.change(await screen.findByRole("textbox", { name: "What do you want to build?" }), { target: { value: "a todo app" } })
    fireEvent.click(screen.getByRole("button", { name: "Plan with claude" }))
    await waitFor(() => expect(post).toHaveBeenCalledWith("/api/flow/plan", { idea: "a todo app" }))
    expect(await screen.findByText("claude is planning…")).toBeInTheDocument()
  })

  it("explains why planning needs claude, and still applies specs written by hand", async () => {
    get.mockResolvedValue({
      data: { ...base, clis: [{ name: "claude", installed: false }, { name: "codex", installed: true }], tasks: [{ id: "F1.1.T1", title: "Say hello", phase: 1, model: "codex/x" }], diff: "+ F1.1.T1" },
    })
    post.mockResolvedValue({ data: { ...base, tasks: [{ id: "F1.1.T1", title: "Say hello", phase: 1, model: "codex/x" }], applied: true, ready: 1, estimate: "≈ 1 task · $0.40" } })
    renderPage()
    expect(await screen.findByText(/runs as Claude Code skills/)).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Plan again" })).toBeDisabled()
    fireEvent.click(screen.getByRole("button", { name: "Apply the plan" }))
    await waitFor(() => expect(post).toHaveBeenCalledWith("/api/flow/apply", undefined))
    expect(await screen.findByText("≈ 1 task · $0.40")).toBeInTheDocument()
    expect(screen.getByRole("link", { name: "Run it from Now" })).toHaveAttribute("href", "/")
  })

  it("speaks Spanish", async () => {
    setLanguage("es")
    get.mockResolvedValue({ data: base })
    const { container } = renderPage()
    await screen.findByRole("textbox")
    expect(englishLeaks(visibleText(container), "es")).toEqual([])
  })
})
