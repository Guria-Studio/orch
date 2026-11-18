import "@testing-library/jest-dom/vitest"
import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { FindingsPage } from "@/pages/FindingsPage"

const sendFinding = vi.hoisted(() => vi.fn(async () => ({ id: 1, status: "sent", url: "u" })))
const dismissFinding = vi.hoisted(() => vi.fn(async () => ({ id: 2, status: "dismissed" })))
vi.mock("@/lib/api", async (importOriginal) => ({ ...(await importOriginal<typeof import("@/lib/api")>()), sendFinding, dismissFinding }))

// mockFindingsData is read by useFindings on every render, so a test can
// flip is_local before rendering without re-mocking the module.
const mockFindingsData = vi.hoisted(() => ({
  repo: "Guria-Studio/orch",
  can_send: true,
  is_local: true,
  pending: 1,
  findings: [
    { id: 2, status: "queued", title: "explain lists backlog as ready", body: "**Type**: bug\n\n## Summary\nexplain says ready", task_id: "F1.T1", queued_at: "2026-11-08T10:00:00Z", url: "" },
    { id: 1, status: "sent", title: "older", body: "x", task_id: "", queued_at: "2026-11-07T10:00:00Z", url: "https://github.com/Guria-Studio/orch/issues/9" },
  ],
}))
vi.mock("@/hooks/useFindings", () => ({
  useFindings: () => ({
    isLoading: false,
    isError: false,
    data: mockFindingsData,
  }),
}))

function renderPage() {
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <FindingsPage />
    </QueryClientProvider>,
  )
}

// #536: the operator sees exactly what would be filed, and only Send files it.
describe("FindingsPage", () => {
  beforeEach(() => {
    mockFindingsData.is_local = true
  })

  it("previews each queued finding and sends or dismisses it on a click", async () => {
    renderPage()
    expect(screen.getByText("explain lists backlog as ready")).toBeInTheDocument()
    expect(screen.getByLabelText("What would be sent")).toHaveTextContent("## Summary explain says ready")
    // Sent ones stay out of the way until asked for.
    expect(screen.queryByText("older")).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    await waitFor(() => expect(sendFinding).toHaveBeenCalledWith(2))
    fireEvent.click(screen.getByRole("button", { name: "Dismiss" }))
    await waitFor(() => expect(dismissFinding).toHaveBeenCalledWith(2))

    fireEvent.click(screen.getByRole("checkbox", { name: "Show sent and dismissed" }))
    expect(screen.getByRole("link", { name: /Sent: https:\/\/github.com\/Guria-Studio\/orch\/issues\/9/ })).toBeInTheDocument()
  })

  // #617: a non-local request (over a tunnel, from another device) gets
  // Send/Dismiss disabled and the exact `orch findings send <id>` hint,
  // instead of a button that would 403 "loopback only" if clicked.
  it("disables Send/Dismiss and shows the orch findings send hint when not local", () => {
    mockFindingsData.is_local = false
    renderPage()

    expect(screen.getByRole("button", { name: "Send" })).toBeDisabled()
    expect(screen.getByRole("button", { name: "Dismiss" })).toBeDisabled()
    expect(screen.getByText(/orch findings send 2/)).toBeInTheDocument()
  })

  // Local behaviour is unchanged: both buttons stay enabled.
  it("leaves Send/Dismiss enabled when the request is local", () => {
    mockFindingsData.is_local = true
    renderPage()

    expect(screen.getByRole("button", { name: "Send" })).not.toBeDisabled()
    expect(screen.getByRole("button", { name: "Dismiss" })).not.toBeDisabled()
    expect(screen.queryByText(/orch findings send 2/)).not.toBeInTheDocument()
  })
})
