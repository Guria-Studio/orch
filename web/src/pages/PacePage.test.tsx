import "@testing-library/jest-dom/vitest"
import { fireEvent, render as rtlRender, screen } from "@testing-library/react"
import type { ReactElement } from "react"
import { MemoryRouter } from "react-router-dom"
import { afterEach, describe, expect, it, vi } from "vitest"
import type { SprintHealth } from "@/lib/types"
import { PacePage } from "@/pages/PacePage"

// Each blocked task links to its page, so the page renders inside a router.
const render = (ui: ReactElement) => rtlRender(<MemoryRouter>{ui}</MemoryRouter>)

const mockUseSprintHealth = vi.fn()
vi.mock("@/hooks/useSprintHealth", () => ({ useSprintHealth: (phase?: string) => mockUseSprintHealth(phase) }))
vi.mock("@/hooks/useMilestones", () => ({
  useMilestones: () => ({ data: [{ phase: 0, name: "Foundation" }, { phase: 1, name: "Core" }] }),
}))

function health(extra: Partial<SprintHealth>): SprintHealth {
  return {
    available: true,
    velocity_per_day: 1.7,
    done_count: 16,
    remaining_tasks: 12,
    remaining_hours: 26.5,
    blocked_count: 2,
    eta_days: 7,
    eta_date: "2026-09-23",
    confidence: "high",
    blockers: [],
    ...extra,
  }
}

describe("PacePage", () => {
  afterEach(() => vi.clearAllMocks())

  // The projected date leaves blocked tasks out; the page has to say how many,
  // with the right plural, or the date reads as covering all remaining work.
  // The design's "Fase" chip narrows the pace to one phase.
  it("narrows the pace to the phase picked", () => {
    mockUseSprintHealth.mockReturnValue({ data: health({}), isLoading: false, isError: false, error: null })
    render(<PacePage />)
    expect(mockUseSprintHealth).toHaveBeenLastCalledWith("")
    fireEvent.change(screen.getByRole("combobox", { name: /Phase/ }), { target: { value: "1" } })
    expect(mockUseSprintHealth).toHaveBeenLastCalledWith("1")
  })

  it("says how many blocked tasks the finish date excludes", () => {
    mockUseSprintHealth.mockReturnValue({ data: health({ blocked_count: 1 }), isLoading: false, isError: false, error: null })
    const { unmount } = render(<PacePage />)
    expect(screen.getByText(/excludes 1 blocked task$/)).toBeInTheDocument()
    unmount()

    mockUseSprintHealth.mockReturnValue({ data: health({ blocked_count: 2 }), isLoading: false, isError: false, error: null })
    render(<PacePage />)
    expect(screen.getByText(/excludes 2 blocked tasks$/)).toBeInTheDocument()
    expect(screen.getByText("Pace = tasks finished in the last 7 days ÷ 7 = 1.7 per day.")).toBeInTheDocument()
  })

  it("shows each blocker with its reason, marked as waiting", () => {
    mockUseSprintHealth.mockReturnValue({
      data: health({
        blockers: [
          { task_id: "F2.T5", title: "Stripe webhook handler", phase: 2, reason: "Waiting on keys", blocked_at: null, estimate_hours: 1 },
        ],
      }),
      isLoading: false,
      isError: false,
      error: null,
    })
    render(<PacePage />)
    expect(screen.getByText("Stripe webhook handler")).toBeInTheDocument()
    expect(screen.getByText("Waiting on keys")).toBeInTheDocument()
    expect(screen.getByRole("link", { name: /Stripe webhook handler/ })).toHaveAttribute("href", "/work/tasks/F2.T5")
    expect(screen.getByText("2 tasks")).toHaveClass("text-status-blocked")
  })
})
