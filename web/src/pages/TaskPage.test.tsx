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
  window.localStorage.clear()
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

  // #405: comments read as author · time · text, never as raw JSON. orch's
  // own status notes are muted beside a person's or an agent's, an operator
  // note says it is one, and an entry that is not a comment breaks nothing.
  it("shows the comments as a thread, not as JSON", async () => {
    const withComments = {
      ...task,
      comments: [
        { at: "2026-10-12T21:31:42Z", author: "orch", body: "in-progress" },
        { at: "2026-10-12T21:35:00Z", author: "claude/claude-sonnet-4-6", body: "Refilled the slot; the scheduler test covers it." },
        { at: "2026-10-12T21:36:00Z", author: "operator", body: "keep the old flag working", kind: "operator_note" },
        "not a comment",
        { at: "2026-10-12T21:41:42Z", author: "orch", body: "done" },
      ],
    }
    get.mockImplementation((url: string) =>
      Promise.resolve({ data: url.includes("/activity") ? { offset: 0, exists: false, steps: [] } : withComments }),
    )
    renderAt([taskPath("F6.1.T1")])

    const list = await screen.findByRole("list", { name: "Comments (5)" })
    expect(list.textContent).not.toContain('"author"')
    const items = list.querySelectorAll("li")
    expect(items).toHaveLength(4)
    expect(items[0]).toHaveTextContent("orch")
    expect(items[0]).toHaveTextContent("in-progress")
    expect(items[0]).toHaveAttribute("data-muted", "true")
    expect(items[1]).toHaveTextContent("Refilled the slot; the scheduler test covers it.")
    expect(items[1]).not.toHaveAttribute("data-muted")
    expect(items[2]).toHaveTextContent("Operator note")
    expect(items[2].querySelector("time")).toHaveAttribute("dateTime", "2026-10-12T21:36:00Z")
  })

  // #404: the details get 70% and the activity 30% by default; the split
  // moves with the keyboard (and the pointer), keeps 20% for each side,
  // resets on a double click, and survives a reload.
  it("splits details and activity 70/30, resizable and remembered", async () => {
    renderAt([taskPath("F6.1.T1")])
    const handle = await screen.findByRole("separator", { name: "Resize the details and the activity" })
    const grid = handle.parentElement!
    expect(handle).toHaveAttribute("aria-valuenow", "70")
    expect(grid.style.getPropertyValue("--details")).toBe("70fr")
    expect(grid.style.getPropertyValue("--activity")).toBe("30fr")

    fireEvent.keyDown(handle, { key: "ArrowLeft" })
    expect(handle).toHaveAttribute("aria-valuenow", "65")
    expect(window.localStorage.getItem("orch.taskSplit")).toBe("65")
    for (let i = 0; i < 20; i++) fireEvent.keyDown(handle, { key: "ArrowLeft" })
    expect(handle).toHaveAttribute("aria-valuenow", "20")

    fireEvent.doubleClick(handle)
    expect(handle).toHaveAttribute("aria-valuenow", "70")
    expect(grid.style.getPropertyValue("--activity")).toBe("30fr")
  })

  it("opens at the split the operator left", async () => {
    window.localStorage.setItem("orch.taskSplit", "55")
    renderAt([taskPath("F6.1.T1")])
    expect(await screen.findByRole("separator", { name: "Resize the details and the activity" })).toHaveAttribute("aria-valuenow", "55")
  })

  it("goes to the list when it was opened straight from a link", async () => {
    renderAt([taskPath("F6.1.T1")])
    fireEvent.click(await screen.findByRole("button", { name: "Back" }))
    expect(location()).toBe("/work/list")
  })
})
