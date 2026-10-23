import "@testing-library/jest-dom/vitest"
import { fireEvent, render, screen } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { MemoryRouter } from "react-router-dom"
import { describe, expect, it, vi } from "vitest"
import { ClientPage } from "@/pages/ClientPage"
import example from "@/stakeholder/data.example.json"

const get = vi.hoisted(() => vi.fn())
vi.mock("@/lib/api", () => ({ apiClient: { get } }))

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <ClientPage />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe("ClientPage", () => {
  // What the client portal showed, now inside the backoffice: the status in a
  // sentence and the roadmap by phase name, from the portal's own snapshot.
  it("shows the client's status and roadmap from the portal snapshot", async () => {
    get.mockResolvedValue({ data: { ...example, refresh_interval_s: 0 } })
    renderPage()
    expect(await screen.findByRole("heading", { level: 1, name: "Example Project" })).toBeInTheDocument()
    expect(get).toHaveBeenCalledWith("/stakeholder/data.json")
    expect(screen.getByRole("progressbar")).toHaveAttribute("aria-valuenow", "24")
    fireEvent.click(screen.getByRole("tab", { name: "Roadmap" }))
    expect(screen.getByText("Core")).toBeInTheDocument()
  })
})
