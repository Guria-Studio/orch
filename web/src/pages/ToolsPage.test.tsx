import "@testing-library/jest-dom/vitest"
import { fireEvent, render, screen, within } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import type { Tool, UnresolvedCommand } from "@/hooks/useTools"
import { ToolsPage } from "@/pages/ToolsPage"

const mockUseTools = vi.fn()
vi.mock("@/hooks/useTools", () => ({ useTools: () => mockUseTools() }))

function loaded(tools: Tool[], commands: UnresolvedCommand[] = []) {
  mockUseTools.mockReturnValue({ data: { tools, commands }, isLoading: false, isError: false, isFetching: false, refetch: vi.fn() })
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

  // The same words as orch doctor (#440): a configured command the run would
  // find only mid-task.
  it("names a configured command whose program is not installed", () => {
    loaded([], [{ key: "verify.app.start", command: "tue", detail: 'verify.app.start: "tue" not found on PATH' }])
    render(<ToolsPage />)
    const alert = screen.getByRole("alert")
    expect(within(alert).getByText("A command in config.yaml will not run")).toBeInTheDocument()
    expect(within(alert).getByText('verify.app.start: "tue" not found on PATH')).toBeInTheDocument()
  })

  // #448: an agent CLI behind its latest release says so, with the command
  // that updates it the way it was installed.
  it("shows an outdated agent CLI's installed and latest version and how to update it", () => {
    loaded([
      { id: "claude", group: "agents", used: true, found: true, version: "2.1.290 (Claude Code)", latest: "2.1.301", update: "npm install -g @anthropic-ai/claude-code@latest" },
      { id: "codex", group: "agents", used: true, found: true, version: "codex-cli 0.154.0", latest: "0.154.0" },
    ])
    render(<ToolsPage />)
    const agents = screen.getByRole("region", { name: "Agents" })
    expect(within(agents).getByText("Update available")).toBeInTheDocument()
    expect(within(agents).getByText("2.1.290 (Claude Code) → 2.1.301")).toBeInTheDocument()
    expect(within(agents).getByText("Installed")).toBeInTheDocument()
    fireEvent.click(within(agents).getByRole("button", { name: "How to update" }))
    expect(within(agents).getByText("npm install -g @anthropic-ai/claude-code@latest")).toBeInTheDocument()
  })
})
