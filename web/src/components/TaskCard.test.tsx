import "@testing-library/jest-dom/vitest"
import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { TaskCard } from "@/components/TaskCard"
import type { Task } from "@/lib/types"

const task = {
  id: "T-1", title: "First", status: "done", phase: 1, model: "claude/haiku", estimate_hours: 1,
  dependencies: [], files: [], comments: [],
} as unknown as Task

describe("TaskCard", () => {
  // vcs.provider: local (#455): the card says the work is on the base, where a PR would be.
  it("says a task was merged locally, and nothing for one that was not", () => {
    const { rerender } = render(<TaskCard task={{ ...task, merged_locally: true }} />)
    expect(screen.getByText("Merged locally")).toBeInTheDocument()
    rerender(<TaskCard task={task} />)
    expect(screen.queryByText("Merged locally")).not.toBeInTheDocument()
  })
})
