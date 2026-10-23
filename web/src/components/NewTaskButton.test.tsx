import "@testing-library/jest-dom/vitest"
import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom"
import { describe, expect, it, vi } from "vitest"
import { NewTaskButton } from "@/components/NewTaskButton"

const addTask = vi.hoisted(() => vi.fn())
vi.mock("@/lib/api", () => ({ addTask }))

function Where() {
  return <output aria-label="location">{useLocation().pathname}</output>
}

function renderButton() {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <MemoryRouter>
        <Routes>
          <Route path="*" element={<><NewTaskButton /><Where /></>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
  fireEvent.click(screen.getByRole("button", { name: "Task" }))
}

describe("NewTaskButton", () => {
  it("adds the task through the API and opens it", async () => {
    addTask.mockResolvedValue({ task_id: "F2.T9", status: "todo" })
    renderButton()
    fireEvent.change(screen.getByRole("textbox", { name: "ID" }), { target: { value: "F2.T9" } })
    fireEvent.change(screen.getByRole("textbox", { name: "Title" }), { target: { value: "Retry webhooks" } })
    fireEvent.change(screen.getByRole("textbox", { name: "Depends on" }), { target: { value: "F2.T5, F2.T6" } })
    fireEvent.click(screen.getByRole("button", { name: "Create task" }))
    await waitFor(() => expect(screen.getByRole("status", { name: "location" })).toHaveTextContent("/work/tasks/F2.T9"))
    expect(addTask).toHaveBeenCalledWith(expect.objectContaining({ id: "F2.T9", title: "Retry webhooks", dependencies: ["F2.T5", "F2.T6"], backlog: false }))
  })

  it("shows the server's reason when it refuses", async () => {
    addTask.mockRejectedValue(new Error("unknown dependency F9.T1"))
    renderButton()
    fireEvent.change(screen.getByRole("textbox", { name: "ID" }), { target: { value: "X" } })
    fireEvent.change(screen.getByRole("textbox", { name: "Title" }), { target: { value: "x" } })
    fireEvent.click(screen.getByRole("button", { name: "Create task" }))
    expect(await screen.findByRole("alert")).toHaveTextContent("unknown dependency F9.T1")
  })
})
