import "@testing-library/jest-dom/vitest"
import { render, screen, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { ActivityFeed } from "@/components/ActivityFeed"

const get = vi.fn()
vi.mock("@/lib/api", () => ({ apiClient: { get: (...args: unknown[]) => get(...args) } }))

function reply(data: unknown) {
  return Promise.resolve({ data })
}

describe("ActivityFeed", () => {
  beforeEach(() => get.mockReset())
  afterEach(() => vi.useRealTimers())

  it("follows the log from the offset it was handed, and shows the current attempt only", async () => {
    get
      .mockReturnValueOnce(
        reply({
          offset: 100,
          exists: true,
          steps: [
            { kind: "start" },
            { kind: "tool", tool: "Bash", detail: "make test" },
            { kind: "error", detail: "make test exited 2" },
            // a retry opens a new attempt in the same log
            { kind: "start" },
            { kind: "tool", tool: "Read", detail: "internal/engine/scheduler.go" },
          ],
        }),
      )
      .mockReturnValueOnce(
        reply({ offset: 180, exists: true, steps: [{ kind: "text", detail: "The scheduler drops the slot twice." }] }),
      )
      .mockReturnValue(reply({ offset: 180, exists: true, steps: [] }))

    render(<ActivityFeed taskId="F2.T3" live />)

    expect(await screen.findByText("internal/engine/scheduler.go")).toBeInTheDocument()
    expect(screen.queryByText("make test")).not.toBeInTheDocument()
    expect(get).toHaveBeenNthCalledWith(1, "/api/task/F2.T3/activity", { params: undefined })

    expect(await screen.findByText("The scheduler drops the slot twice.", {}, { timeout: 4_000 })).toBeInTheDocument()
    expect(get).toHaveBeenNthCalledWith(2, "/api/task/F2.T3/activity", { params: { offset: 100 } })
  })

  it("says when a task has no log yet, and when its provider does not stream", async () => {
    get.mockReturnValue(reply({ offset: 0, exists: false, steps: [] }))
    const { unmount } = render(<ActivityFeed taskId="T-1" live={false} />)
    expect(await screen.findByText("This task has not been dispatched yet.")).toBeInTheDocument()
    unmount()

    get.mockReturnValue(reply({ offset: 40, exists: true, steps: [] }))
    render(<ActivityFeed taskId="T-2" live={false} provider="gemini" />)
    expect(await screen.findByText(/gemini reports only when it finishes/)).toBeInTheDocument()
  })

  it("reads once when the task is not running", async () => {
    get.mockReturnValue(reply({ offset: 10, exists: true, steps: [{ kind: "end", detail: "success" }] }))
    render(<ActivityFeed taskId="T-1" live={false} />)
    expect(await screen.findByText("Finished")).toBeInTheDocument()
    await new Promise((r) => setTimeout(r, 2_500))
    await waitFor(() => expect(get).toHaveBeenCalledTimes(1))
  })
})
