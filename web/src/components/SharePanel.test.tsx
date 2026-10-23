import "@testing-library/jest-dom/vitest"
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { afterEach, describe, expect, it, vi } from "vitest"
import { SharePanel } from "@/components/SharePanel"
import type { TunnelCapabilities, TunnelProvider, TunnelStatus } from "@/lib/types"

const api = vi.hoisted(() => ({
  getTunnelCapabilities: vi.fn(),
  getTunnelStatus: vi.fn(),
  startTunnel: vi.fn(),
  stopTunnel: vi.fn(),
}))
vi.mock("@/lib/api", () => api)

const guide = (id: string, command: string) => ({ id, label: id, steps: [{ title: "Install", command }] })

function provider(id: TunnelProvider["id"], found: boolean): TunnelProvider {
  const tailscale = id !== "cloudflared"
  return {
    id,
    command: tailscale ? "tailscale" : "cloudflared",
    binary: { found, version: found ? "1.0.0" : undefined },
    guides: tailscale ? [guide("tailscale-linux", "curl -fsSL https://tailscale.com/install.sh | sh")] : [guide("apt", "sudo apt-get install cloudflared")],
    recommended_guide: tailscale ? "tailscale-linux" : "apt",
    docs_url: "https://example.test/download",
  }
}

function caps(providers: TunnelProvider[]): TunnelCapabilities {
  return {
    enabled: true,
    provider: "cloudflared",
    can_control: true,
    reason: "ok",
    reasons: [],
    providers,
    repo: { remote: "github.com:hectorcanaimero/orch.git", host: "github.com", slug: "hectorcanaimero/orch", branch: "main" },
    local_url: "http://127.0.0.1:7420",
  }
}

const idle: TunnelStatus = {
  state: "idle", provider: null, pid: null, url: null, started_at: null, url_at: null, restart_count: 0,
  last_error: null, last_exit_code: null, phase: "idle", share_url: null, portal_url: null, log_tail: [],
}

function renderPanel() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <SharePanel />
    </QueryClientProvider>,
  )
}

describe("SharePanel", () => {
  afterEach(() => vi.clearAllMocks())

  it("names the repository, branch and local address the dashboard serves", async () => {
    api.getTunnelCapabilities.mockResolvedValue(caps([provider("cloudflared", true), provider("tailscale-serve", false), provider("tailscale-funnel", false)]))
    api.getTunnelStatus.mockResolvedValue(idle)
    renderPanel()
    expect(await screen.findByText("hectorcanaimero/orch")).toBeInTheDocument()
    expect(screen.getByText("main")).toBeInTheDocument()
    expect(screen.getByText("http://127.0.0.1:7420")).toBeInTheDocument()
    expect(screen.getAllByRole("radio")).toHaveLength(3)
  })

  // A program that is missing says so and shows how to install it, instead of
  // a start button that would only fail.
  it("shows the install steps for a provider that is not installed", async () => {
    api.getTunnelCapabilities.mockResolvedValue(caps([provider("cloudflared", true), provider("tailscale-serve", false), provider("tailscale-funnel", false)]))
    api.getTunnelStatus.mockResolvedValue(idle)
    renderPanel()
    fireEvent.click(await screen.findByRole("radio", { name: /Tailscale · tailnet/ }))
    const install = screen.getByRole("region", { name: "tailscale is not installed on this machine" })
    expect(within(install).getByText("curl -fsSL https://tailscale.com/install.sh | sh")).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Generate links" })).not.toBeInTheDocument()
  })

  it("starts the share through the chosen provider", async () => {
    api.getTunnelCapabilities.mockResolvedValue(caps([provider("cloudflared", true), provider("tailscale-serve", true), provider("tailscale-funnel", true)]))
    api.getTunnelStatus.mockResolvedValue(idle)
    api.startTunnel.mockResolvedValue({ ...idle, state: "starting", provider: "tailscale-funnel" })
    renderPanel()
    fireEvent.click(await screen.findByRole("radio", { name: /Tailscale · public/ }))
    fireEvent.click(screen.getByRole("button", { name: "Generate links" }))
    await waitFor(() => expect(api.startTunnel).toHaveBeenCalledWith("tailscale-funnel"))
  })

  // While a share is up, it is the one shown, with both links; the other
  // providers wait until it is stopped.
  it("shows the running share's links and locks the other providers", async () => {
    api.getTunnelCapabilities.mockResolvedValue(caps([provider("cloudflared", true), provider("tailscale-serve", true), provider("tailscale-funnel", true)]))
    api.getTunnelStatus.mockResolvedValue({
      ...idle, state: "running", provider: "tailscale-serve", url: "https://box.tail1.ts.net", started_at: new Date().toISOString(),
      share_url: "https://box.tail1.ts.net/?token=op", portal_url: "https://box.tail1.ts.net/?token=client",
    })
    renderPanel()
    expect(await screen.findByText("https://box.tail1.ts.net/?token=client")).toBeInTheDocument()
    expect(screen.getByText("https://box.tail1.ts.net/?token=op")).toBeInTheDocument()
    expect(screen.getByRole("radio", { name: /Tailscale · tailnet/ })).toBeChecked()
    expect(screen.getByRole("radio", { name: /Cloudflare/ })).toBeDisabled()
  })
})
