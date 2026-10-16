import "@testing-library/jest-dom/vitest"
import { act, fireEvent, render, screen } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { TaskSteer } from "@/components/TaskSteer"
import type { Task } from "@/lib/types"

const whoami = vi.fn()
vi.mock("@/hooks/useWhoami", () => ({ useWhoami: () => whoami() }))
const stopTask = vi.fn()
const noteTask = vi.fn()
vi.mock("@/lib/api", () => ({
  stopTask: (...a: unknown[]) => stopTask(...a),
  noteTask: (...a: unknown[]) => noteTask(...a),
}))

const task = { id: "F1.T2", status: "in-progress" } as Task

function renderSteer(t: Task = task) {
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <TaskSteer task={t} />
    </QueryClientProvider>,
  )
}

beforeEach(() => {
  whoami.mockReturnValue({ data: { profile: "operator" } })
  stopTask.mockReset()
  noteTask.mockReset()
})

describe("TaskSteer", () => {
  it("is not shown to a stakeholder", () => {
    whoami.mockReturnValue({ data: { profile: "stakeholder" } })
    const { container } = renderSteer()
    expect(container).toBeEmptyDOMElement()
  })

  it("stops and holds a running task with the note, and shows the answer", async () => {
    stopTask.mockResolvedValue({ message: "stop requested: the running orch run ends F1.T2's attempt" })
    renderSteer()
    fireEvent.change(screen.getByRole("textbox"), { target: { value: "wait for the API" } })
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Stop and hold" }))
    })
    expect(stopTask).toHaveBeenCalledWith("F1.T2", true, "wait for the API")
    expect(screen.getByRole("status")).toHaveTextContent("stop requested")
  })

  it("offers only the note on a task that is not running, and shows a refusal", async () => {
    noteTask.mockRejectedValue(new Error("loopback only"))
    renderSteer({ ...task, status: "todo" } as Task)
    expect(screen.queryByRole("button", { name: "Stop and hold" })).toBeNull()
    fireEvent.change(screen.getByRole("textbox"), { target: { value: "use uv" } })
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Add note" }))
    })
    expect(noteTask).toHaveBeenCalledWith("F1.T2", "use uv")
    expect(screen.getByRole("alert")).toHaveTextContent("loopback only")
  })
})
