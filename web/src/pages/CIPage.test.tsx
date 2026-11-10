import "@testing-library/jest-dom/vitest"
import { render as rtlRender, screen, within } from "@testing-library/react"
import type { ReactElement } from "react"
import { MemoryRouter } from "react-router-dom"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { setLanguage } from "@/i18n"
import { englishLeaks, visibleText } from "@/i18n/leaks"
import type { CiResponse } from "@/lib/ci"
import { CIPage } from "@/pages/CIPage"

const render = (ui: ReactElement) => rtlRender(<MemoryRouter>{ui}</MemoryRouter>)

const state: { ci: CiResponse | undefined } = { ci: undefined }
vi.mock("@/hooks/useCI", () => ({
  useCI: () => ({ data: state.ci, isLoading: false, isError: false, error: null }),
}))
vi.mock("@/hooks/useTasks", () => ({
  useTasks: () => ({
    data: { tasks: [{ id: "F1.T2", title: "Login form", status: "in-progress" }, { id: "F1.T1", title: "Schema", status: "done" }] },
    isLoading: false,
    isError: false,
    error: null,
  }),
}))

const base: CiResponse = {
  config: {
    worktree_mode: true, base_branch: "main", provider: "github", host: "", auto_pr: true,
    ci_max_retries: 1, ci_poll_interval_s: 30, auto_merge: true, test_command: "go test ./...",
  },
  workflows: [],
  warnings: [],
}

beforeEach(() => {
  state.ci = {
    ...base,
    merge_queue: {
      enabled: true,
      reverify: "when_base_moved",
      entries: [
        { task_id: "F1.T2", pr_url: "https://github.com/o/r/pull/12", state: "queued", position: 1, requeues: 1,
          enqueued_at: "2026-11-09T12:00:00Z", updated_at: "2026-11-09T12:00:00Z" },
        { task_id: "F1.T1", pr_url: "https://github.com/o/r/pull/11", state: "merged", requeues: 0,
          enqueued_at: "2026-11-09T11:00:00Z", updated_at: "2026-11-09T11:30:00Z" },
      ],
    },
  }
})

afterEach(() => setLanguage("en"))

describe("CIPage merge queue", () => {
  it("lists the queue in merge order, then what left it", () => {
    render(<CIPage />)
    const list = screen.getByRole("list", { name: "Merge queue" })
    const rows = within(list).getAllByRole("listitem")
    expect(rows).toHaveLength(2)
    expect(rows[0]).toHaveTextContent("next")
    expect(rows[0]).toHaveTextContent("Login form")
    expect(rows[0]).toHaveTextContent("Queued")
    expect(rows[0]).toHaveTextContent("1 time(s) sent back")
    expect(rows[1]).toHaveTextContent("Merged")
    expect(screen.getByText(/the local verify runs again on it, without the review/)).toBeInTheDocument()
  })

  it("says the queue is off and how to turn it on", () => {
    state.ci = { ...base, merge_queue: { enabled: false, reverify: "when_base_moved", entries: [] } }
    render(<CIPage />)
    expect(screen.getByText(/merge_queue.enabled turns the queue on/)).toBeInTheDocument()
    expect(screen.queryByRole("list", { name: "Merge queue" })).not.toBeInTheDocument()
  })

  it.each(["es", "pt"] as const)("speaks %s", (lang) => {
    setLanguage(lang)
    const { container } = render(<CIPage />)
    // The queue's own words; the pipeline cards name config keys on purpose.
    const section = container.querySelector('section[aria-labelledby="queue-heading"]') as HTMLElement
    expect(englishLeaks(visibleText(section), lang)).toEqual([])
  })
})
