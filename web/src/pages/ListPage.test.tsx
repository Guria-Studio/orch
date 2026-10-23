import "@testing-library/jest-dom/vitest"
import { render, screen, within } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { MemoryRouter } from "react-router-dom"
import { describe, expect, it, vi } from "vitest"
import { ListPage } from "@/pages/ListPage"

const task = (id: string, status: string, extra: Record<string, unknown> = {}) => ({
  id, title: `Task ${id}`, status, phase: 1, model: "claude/claude-sonnet-4-6", dependencies: [], dep_count: 0,
  estimate_hours: 2, files: [], spec_ref: "", comments: [], human_hours: 0, last_updated: "", description: "", reason: "",
  downstream_impact: 0, on_critical_path: false, parallelizable: false, ...extra,
})

vi.mock("@/hooks/useTasks", () => ({
  useTasks: () => ({
    isLoading: false,
    isError: false,
    data: { count: 3, total: 3, tasks: [task("F1.T2", "done"), task("F1.T1", "in-progress"), task("F1.T3", "done")] },
  }),
}))
vi.mock("@/hooks/useEventStream", () => ({ useEventStream: () => ({ status: "open", lastEventAt: null }) }))

describe("ListPage", () => {
  // The approved Linear list: one group per status, what is moving first,
  // each row a link to its task.
  it("groups tasks by status, in progress first, each row linking to its page", () => {
    render(
      <QueryClientProvider client={new QueryClient()}>
      <MemoryRouter>
        <ListPage />
      </MemoryRouter>
      </QueryClientProvider>,
    )
    const lists = screen.getAllByRole("list")
    expect(lists.map((l) => l.getAttribute("aria-label"))).toEqual(["In progress", "Done"])
    expect(within(lists[1]).getAllByRole("link").map((a) => a.getAttribute("href"))).toEqual(["/work/tasks/F1.T2", "/work/tasks/F1.T3"])
    expect(within(lists[0]).getByRole("link")).toHaveTextContent("Task F1.T1")
  })
})
