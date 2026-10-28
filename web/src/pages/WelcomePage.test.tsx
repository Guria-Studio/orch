import "@testing-library/jest-dom/vitest"
import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { afterEach, describe, expect, it, vi } from "vitest"
import { WelcomePage, type WelcomeState } from "@/pages/WelcomePage"

const get = vi.fn()
const post = vi.fn()
vi.mock("@/lib/api", () => ({ apiClient: { get: (...a: unknown[]) => get(...a), post: (...a: unknown[]) => post(...a) } }))

const base: WelcomeState = {
  clis: [
    { name: "claude", installed: true },
    { name: "codex", installed: false },
  ],
  cwd: "/home/me/app",
  root: "",
  log: [],
  tasks: [],
  planning: false,
  steps: [],
  applied: false,
  ready: 0,
}

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <WelcomePage />
    </QueryClientProvider>,
  )
}

describe("WelcomePage", () => {
  afterEach(() => vi.clearAllMocks())

  it("starts at the folder, suggesting the working directory, and posts it", async () => {
    get.mockResolvedValue({ data: base })
    post.mockResolvedValue({ data: { ...base, root: "/home/me/app", log: ["✓ orch project created"] } })
    renderPage()
    const folder = await screen.findByRole("textbox", { name: "The project's folder" })
    expect(folder).toHaveValue("/home/me/app")
    expect(screen.getByRole("button", { name: "Plan with claude" })).toBeDisabled()

    fireEvent.click(screen.getByRole("button", { name: "Use this folder" }))
    await waitFor(() => expect(post).toHaveBeenCalledWith("/api/welcome/project", { path: "/home/me/app", git_init: true }))
    expect(await screen.findByText("✓ orch project created")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Plan with claude" })).toBeEnabled()
  })

  it("shows the planned tasks, then the estimate and the run button once applied", async () => {
    get.mockResolvedValue({ data: { ...base, root: "/a", tasks: [{ id: "F1.1.T1", title: "Say hello", phase: 1, model: "claude/x" }] } })
    post.mockResolvedValue({ data: { ...base, root: "/a", tasks: [{ id: "F1.1.T1", title: "Say hello", phase: 1, model: "claude/x" }], applied: true, ready: 1, estimate: "≈ 1 task · $0.40" } })
    renderPage()
    expect(await screen.findByText("Say hello")).toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "Apply the plan" }))
    expect(await screen.findByText("≈ 1 task · $0.40")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Run the first task" })).toBeEnabled()
  })

  it("says what a refused step answered", async () => {
    get.mockResolvedValue({ data: base })
    post.mockRejectedValue({ response: { data: { detail: "/x is not a git repository" } } })
    renderPage()
    fireEvent.click(await screen.findByRole("button", { name: "Use this folder" }))
    expect(await screen.findByRole("alert")).toHaveTextContent("/x is not a git repository")
  })
})
