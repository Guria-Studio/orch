import "@testing-library/jest-dom/vitest"
import { fireEvent, render, screen } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { taskPath } from "@/lib/nav"
import { TaskPage } from "@/pages/TaskPage"
import type { Task } from "@/lib/types"

const longCall = `go test ./internal/engine/... -run '${"TestSchedulerRefillsTheSlot".repeat(10)}' -race -count=1`

const task = {
  id: "F6.1.T1",
  title: "Refill the slot once",
  status: "in-progress",
  phase: 6,
  model: "claude/claude-sonnet-4-6",
  estimate_hours: 2,
  human_hours: 1,
  downstream_impact: 3,
  parallelizable: true,
  last_updated: "2026-10-18T03:00:00Z",
  description: "",
  reason: "",
  dependencies: [],
  files: [],
  spec_ref: "",
  comments: [],
} as unknown as Task

const get = vi.fn()
vi.mock("@/lib/api", () => ({ apiClient: { get: (...args: unknown[]) => get(...args) } }))
vi.mock("@/hooks/useWhoami", () => ({ useWhoami: () => ({ data: { profile: "stakeholder" } }) }))

function Where() {
  const { pathname, search } = useLocation()
  return <output aria-label="location">{pathname + search}</output>
}

function renderAt(entries: string[]) {
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <MemoryRouter initialEntries={entries} initialIndex={entries.length - 1}>
        <Routes>
          <Route path="/work/tasks/:id" element={<TaskPage />} />
          <Route path="*" element={<p>board</p>} />
        </Routes>
        <Where />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

const location = () => screen.getByRole("status", { name: "location" }).textContent

beforeEach(() => {
  get.mockReset()
  get.mockImplementation((url: string) =>
    Promise.resolve({
      data: url.startsWith("/api/task/F6.1.T1/activity")
        ? { offset: 10, exists: true, steps: [{ kind: "tool", tool: "Bash", detail: longCall }] }
        : task,
    }),
  )
})

describe("TaskPage", () => {
  it("opens a task straight from its address, dots in the id and all", async () => {
    renderAt([taskPath("F6.1.T1")])
    expect(await screen.findByRole("heading", { name: "Refill the slot once" })).toBeInTheDocument()
    expect(get).toHaveBeenCalledWith("/api/task/F6.1.T1")
  })

  it("wraps a long tool call in the feed instead of cutting it", async () => {
    renderAt([taskPath("F6.1.T1")])
    const line = await screen.findByText(longCall)
    expect(line).toHaveClass("break-all")
    expect(line).not.toHaveClass("truncate")
  })

  it("goes back to the board it came from, filters and all", async () => {
    renderAt(["/work/kanban?status=blocked", taskPath("F6.1.T1")])
    fireEvent.click(await screen.findByRole("button", { name: "Back" }))
    expect(location()).toBe("/work/kanban?status=blocked")
  })

  it("goes to the list when it was opened straight from a link", async () => {
    renderAt([taskPath("F6.1.T1")])
    fireEvent.click(await screen.findByRole("button", { name: "Back" }))
    expect(location()).toBe("/work/list")
  })
})
