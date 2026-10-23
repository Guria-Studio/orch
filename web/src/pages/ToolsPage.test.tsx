import "@testing-library/jest-dom/vitest"
import { render, screen, within } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import type { Tool } from "@/hooks/useTools"
import { ToolsPage } from "@/pages/ToolsPage"

const mockUseTools = vi.fn()
vi.mock("@/hooks/useTools", () => ({ useTools: () => mockUseTools() }))

function loaded(tools: Tool[]) {
  mockUseTools.mockReturnValue({ data: { tools }, isLoading: false, isError: false, isFetching: false, refetch: vi.fn() })
}

describe("ToolsPage", () => {
  afterEach(() => vi.clearAllMocks())

  // As in the design: a tool this project uses and lacks is counted and opens
  // on how to install it; one it does not use says so and offers nothing.
  it("counts what this project lacks and shows how to install it", () => {
    loaded([
      { id: "claude", group: "agents", used: true, found: true, version: "2.1.301" },
      { id: "tailscale", group: "share", used: true, found: false, install: "curl -fsSL https://tailscale.com/install.sh | sh" },
      { id: "playwright", group: "quality", used: false, found: false, install: "npx playwright install" },
    ])
    render(<ToolsPage />)
    expect(screen.getByText("1 missing for features you use")).toBeInTheDocument()

    const share = screen.getByRole("region", { name: "Sharing and delivery" })
    expect(within(share).getByText("curl -fsSL https://tailscale.com/install.sh | sh")).toBeInTheDocument()

    const quality = screen.getByRole("region", { name: "Quality" })
    expect(within(quality).getByText("Not used")).toBeInTheDocument()
    expect(within(quality).queryByRole("button", { name: "How to install" })).not.toBeInTheDocument()

    expect(within(screen.getByRole("region", { name: "Agents" })).getByText("Installed")).toBeInTheDocument()
  })

  it("shows no count when everything this project uses is installed", () => {
    loaded([{ id: "git", group: "repo", used: true, found: true }])
    render(<ToolsPage />)
    expect(screen.queryByText(/missing for features/)).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "How to install" })).not.toBeInTheDocument()
  })
})
