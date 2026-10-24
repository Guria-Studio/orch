import "@testing-library/jest-dom/vitest"
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { MemoryRouter } from "react-router-dom"
import { afterEach, describe, expect, it, vi } from "vitest"
import type { Lesson } from "@/hooks/useSystem"
import { KnowledgePage } from "@/pages/KnowledgePage"

const mockUseKnowledge = vi.fn()
vi.mock("@/hooks/useSystem", () => ({ useKnowledge: (q: string) => mockUseKnowledge(q) }))
const addLesson = vi.fn()
const editLesson = vi.fn()
const removeLesson = vi.fn()
vi.mock("@/lib/api", () => ({
  addLesson: (...a: unknown[]) => addLesson(...a),
  editLesson: (...a: unknown[]) => editLesson(...a),
  removeLesson: (...a: unknown[]) => removeLesson(...a),
}))

const LESSONS: Lesson[] = [
  {
    id: "20261024-run-migrations",
    title: "Run the migrations before the tests",
    area: ["db/"],
    source: "verify",
    task: "F1.T2",
    created: "2026-10-24",
    body: "pytest needs the schema.",
    path: ".orch/knowledge/20261024-run-migrations.md",
    prompts: 3,
    tasks: 2,
    first_try: 1,
  },
  {
    id: "20261020-lint-ruff",
    title: "Lint with ruff",
    source: "operator",
    created: "2026-10-20",
    body: "ruff check, not flake8.",
    path: ".orch/knowledge/20261020-lint-ruff.md",
    prompts: 0,
    tasks: 0,
    first_try: 0,
  },
]

function renderPage() {
  mockUseKnowledge.mockReturnValue({ data: { lessons: LESSONS, sources: ["agent", "verify", "review", "conflict", "operator"] }, isLoading: false, isError: false })
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <MemoryRouter>
        <KnowledgePage />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

const row = (title: string) => screen.getByRole("button", { name: title }).closest("tr") as HTMLElement

describe("KnowledgePage", () => {
  afterEach(() => vi.clearAllMocks())

  it("lists each lesson with its area, source, task, date and how it did", () => {
    renderPage()
    const used = within(row("Run the migrations before the tests"))
    expect(used.getByText("db/")).toBeInTheDocument()
    expect(used.getByText("verify")).toBeInTheDocument()
    expect(used.getByRole("link", { name: "F1.T2" })).toHaveAttribute("href", "/work/tasks/F1.T2")
    expect(used.getByText("2026-10-24")).toBeInTheDocument()
    expect(used.getByText("3")).toBeInTheDocument()
    expect(used.getByText("1 of 2")).toBeInTheDocument()
  })

  it("searches through the server and filters by area and source here", () => {
    renderPage()
    fireEvent.change(screen.getByRole("searchbox", { name: "Search the lessons" }), { target: { value: "ruff" } })
    expect(mockUseKnowledge).toHaveBeenLastCalledWith("ruff")

    fireEvent.change(screen.getByRole("combobox", { name: "Source" }), { target: { value: "operator" } })
    expect(screen.queryByRole("button", { name: "Run the migrations before the tests" })).not.toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Lint with ruff" })).toBeInTheDocument()

    fireEvent.change(screen.getByRole("combobox", { name: "Source" }), { target: { value: "" } })
    fireEvent.change(screen.getByRole("combobox", { name: "Area" }), { target: { value: "db/" } })
    expect(screen.queryByRole("button", { name: "Lint with ruff" })).not.toBeInTheDocument()
  })

  it("edits and deletes a lesson through the knowledge editor", async () => {
    editLesson.mockResolvedValue({ id: LESSONS[1].id })
    removeLesson.mockResolvedValue({ lesson: LESSONS[1].id })
    renderPage()
    fireEvent.click(screen.getByRole("button", { name: "Lint with ruff" }))
    expect(screen.getByText("ruff check, not flake8.")).toBeInTheDocument()

    fireEvent.click(screen.getByRole("button", { name: "Edit" }))
    const dialog = screen.getByRole("dialog")
    fireEvent.change(within(dialog).getByLabelText("Title"), { target: { value: "Lint with ruff only" } })
    fireEvent.change(within(dialog).getByLabelText("Area (paths or globs, comma-separated)"), { target: { value: "src/, tests/**" } })
    fireEvent.click(within(dialog).getByRole("button", { name: "Save" }))
    await waitFor(() =>
      expect(editLesson).toHaveBeenCalledWith(LESSONS[1].id, {
        title: "Lint with ruff only",
        body: "ruff check, not flake8.",
        area: ["src/", "tests/**"],
        tags: [],
        task: undefined,
        source: "operator",
      }),
    )

    vi.spyOn(window, "confirm").mockReturnValue(true)
    fireEvent.click(screen.getByRole("button", { name: "Delete" }))
    await waitFor(() => expect(removeLesson).toHaveBeenCalledWith(LESSONS[1].id))
  })

  it("adds a lesson, and shows the server's refusal", async () => {
    addLesson.mockRejectedValue(new Error("a lesson needs a title and a body"))
    renderPage()
    fireEvent.click(screen.getByRole("button", { name: "Add lesson" }))
    const dialog = screen.getByRole("dialog")
    fireEvent.change(within(dialog).getByLabelText("Title"), { target: { value: "Use the shared network" } })
    fireEvent.change(within(dialog).getByLabelText("What happened and what worked"), { target: { value: "docker network create shared" } })
    fireEvent.click(within(dialog).getByRole("button", { name: "Save" }))
    await waitFor(() => expect(addLesson).toHaveBeenCalledWith(expect.objectContaining({ title: "Use the shared network", source: "operator" })))
    expect(await within(dialog).findByRole("alert")).toHaveTextContent("a lesson needs a title and a body")
  })

  // #452 builds `orch knowledge curate`; until it lands the button says so.
  it("offers Curate now disabled, naming #452", () => {
    renderPage()
    const curate = screen.getByRole("button", { name: "Curate now" })
    expect(curate).toBeDisabled()
    expect(curate).toHaveAccessibleDescription("Arrives with orch knowledge curate (#452)")
  })
})
