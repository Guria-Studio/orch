import "@testing-library/jest-dom/vitest"
import { fireEvent, render, screen, within, waitFor } from "@testing-library/react"
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
  max_attempts: 3,
} as unknown as Task

const get = vi.fn()
const editTask = vi.hoisted(() => vi.fn())
const moveTask = vi.hoisted(() => vi.fn())
let profile = "stakeholder"
vi.mock("@/lib/api", () => ({
  apiClient: { get: (...args: unknown[]) => get(...args) },
  getTaskDiff: (id: string) => get(`/api/task/${id}/diff`).then((r: { data: unknown }) => r.data),
  editTask,
  moveTask,
}))
vi.mock("@/hooks/useWhoami", () => ({ useWhoami: () => ({ data: { profile } }) }))

function Where() {
  const { pathname, search } = useLocation()
  return <output aria-label="location">{pathname + search}</output>
}

function renderAt(entries: string[]) {
  return render(
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
  profile = "stakeholder"
  editTask.mockReset().mockResolvedValue({ message: "ok" })
  moveTask.mockReset().mockResolvedValue({ message: "ok" })
  get.mockReset()
  get.mockImplementation((url: string) =>
    Promise.resolve({
      data: url.startsWith("/api/task/F6.1.T1/activity")
        ? { offset: 10, exists: true, steps: [{ kind: "tool", tool: "Bash", detail: longCall }] }
        : url === "/api/events"
          ? {
              count: 2,
              events: [
                { ts: "2026-10-12T21:30:00Z", task_id: "F6.1.T1", event_type: "dispatch", backend: "claude", human: "", severity: "info", raw: { extra: { attempt: 2, cli_model: "claude-sonnet-4-6" } } },
                { ts: "2026-10-12T21:40:00Z", task_id: "F6.1.T1", event_type: "verify_failed", backend: "claude", human: "", severity: "warn", raw: { extra: { reason: "2 tests failed" } } },
              ],
            }
          : task,
    }),
  )
})

describe("TaskPage", () => {
  // The design's timeline: the task's own events, in words, with the attempt
  // the latest dispatch carried shown among the properties.
  it("folds the properties panel and remembers it", async () => {
    const { unmount } = renderAt([taskPath("F6.1.T1")])
    fireEvent.click(await screen.findByRole("button", { name: "Hide the properties" }))
    expect(screen.queryByRole("combobox", { name: "Status" })).not.toBeInTheDocument()
    expect(window.localStorage.getItem("orch.taskPropsFolded")).toBe("1")
    unmount()
    renderAt([taskPath("F6.1.T1")])
    fireEvent.click(await screen.findByRole("button", { name: "Show the properties" }))
    expect(screen.getByRole("combobox", { name: "Status" })).toBeInTheDocument()
  })

  it("draws the task's events as the activity timeline", async () => {
    renderAt([taskPath("F6.1.T1")])
    expect(await screen.findByText("dispatched attempt 2 to claude-sonnet-4-6")).toBeInTheDocument()
    expect(screen.getByText("2 tests failed")).toBeInTheDocument()
    const props = screen.getByRole("complementary", { name: "Properties" })
    expect(within(props).getByText("2 of 3")).toBeInTheDocument()
    // The running card says how long the attempt has been going, from its dispatch.
    expect(within(props).getByText(/^Running for /)).toBeInTheDocument()
  })

  it("opens a task straight from its address, dots in the id and all", async () => {
    renderAt([taskPath("F6.1.T1")])
    expect(await screen.findByRole("heading", { name: "Refill the slot once" })).toBeInTheDocument()
    expect(get).toHaveBeenCalledWith("/api/task/F6.1.T1")
  })

  it("wraps a long tool call in the feed instead of cutting it", async () => {
    renderAt([taskPath("F6.1.T1")])
    fireEvent.click(await screen.findByRole("tab", { name: "Agent" }))
    const line = await screen.findByText(longCall)
    expect(line).toHaveClass("break-all")
    expect(line).not.toHaveClass("truncate")
  })

  // #428: the description reads as markdown, not raw backticks.
  it("renders the description as markdown", async () => {
    get.mockImplementation((url: string) => Promise.resolve({ data: url === "/api/task/F6.1.T1" ? { ...task, description: "Edit `config.ts` **now**." } : { events: [], steps: [] } }))
    renderAt([taskPath("F6.1.T1")])
    expect((await screen.findByText("config.ts")).tagName).toBe("CODE")
    expect(screen.getByText("now").tagName).toBe("STRONG")
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

  // #421: the properties are edited in place; a change goes through the same
  // editor as `orch task edit`, a status through the board's move.
  it("saves an edited estimate and moves the status, for the local operator", async () => {
    profile = "operator"
    renderAt([taskPath("F6.1.T1")])
    const estimate = await screen.findByRole("spinbutton", { name: "Est." })
    fireEvent.change(estimate, { target: { value: "3.5" } })
    fireEvent.blur(estimate)
    await waitFor(() => expect(editTask).toHaveBeenCalledWith("F6.1.T1", { estimate_hours: 3.5 }))

    // A running task is the run's: stop it first, the server says.
    expect(screen.getByRole("combobox", { name: "Status" })).toBeDisabled()
  })

  it("moves a task that is not running to another status", async () => {
    profile = "operator"
    get.mockImplementation((url: string) =>
      Promise.resolve({ data: url.includes("/activity") ? { offset: 0, exists: false, steps: [] } : { ...task, status: "todo" } }),
    )
    renderAt([taskPath("F6.1.T1")])
    fireEvent.change(await screen.findByRole("combobox", { name: "Status" }), { target: { value: "done" } })
    await waitFor(() => expect(moveTask).toHaveBeenCalledWith("F6.1.T1", "done", ""))
  })

  it("shows the server's reason when an edit is refused", async () => {
    profile = "operator"
    editTask.mockRejectedValueOnce(new Error("F6.1.T1 depends on itself"))
    renderAt([taskPath("F6.1.T1")])
    const estimate = await screen.findByRole("spinbutton", { name: "Est." })
    fireEvent.change(estimate, { target: { value: "1" } })
    fireEvent.blur(estimate)
    expect(await screen.findByRole("alert")).toHaveTextContent("F6.1.T1 depends on itself")
  })

  it("is read-only for a stakeholder", async () => {
    renderAt([taskPath("F6.1.T1")])
    expect(await screen.findByRole("spinbutton", { name: "Est." })).toBeDisabled()
    expect(screen.queryByRole("button", { name: "Edit prompt" })).not.toBeInTheDocument()
  })

  // #454: the Changes tab is the operator's; a stakeholder sees it only when
  // the operator put api_task_diff on their allow-list.
  it("shows the Changes tab to the operator, not to a stakeholder", async () => {
    const { unmount } = renderAt([taskPath("F6.1.T1")])
    await screen.findByRole("heading", { name: "Refill the slot once" })
    expect(screen.queryByRole("tab", { name: "Changes" })).not.toBeInTheDocument()
    unmount()

    profile = "operator"
    get.mockImplementation((url: string) =>
      Promise.resolve({ data: url.endsWith("/diff") ? { task_id: "F6.1.T1", base: "main", branch: "orch/F6.1.T1", files: [] } : url.includes("/activity") ? { steps: [] } : task }),
    )
    renderAt([taskPath("F6.1.T1")])
    fireEvent.click(await screen.findByRole("tab", { name: "Changes" }))
    expect(await screen.findByText("The branch changes nothing against its base.")).toBeInTheDocument()
  })

  it("goes to the list when it was opened straight from a link", async () => {
    renderAt([taskPath("F6.1.T1")])
    fireEvent.click(await screen.findByRole("button", { name: "Back" }))
    expect(location()).toBe("/work/list")
  })
})
