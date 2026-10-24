import "@testing-library/jest-dom/vitest"
import { fireEvent, render, screen, within } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom"
import { describe, expect, it, vi } from "vitest"
import { AppLayout } from "@/components/AppLayout"

let whoami: { profile: string; routes?: string[] } = { profile: "operator" }
vi.mock("@/hooks/useWhoami", () => ({ useWhoami: () => ({ data: whoami }) }))
vi.mock("@/hooks/usePortfolio", () => ({
  usePortfolio: () => ({ data: undefined, error: undefined }),
  isPortfolioNavVisible: () => false,
}))
vi.mock("@/hooks/useAuth", () => ({ useAuth: () => ({ clearToken: vi.fn() }) }))
vi.mock("@/hooks/useReleases", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/hooks/useReleases")>()),
  useReleases: () => ({
    data: { current: "v0.17.0", releases: [{ tag: "v0.17.0", url: "", critical: false, notes: "", published_at: "", newer: false, current: true }] },
  }),
}))
vi.mock("@/pages/LogsPage", () => ({ LogsPage: () => <p>event log</p> }))
// The layout reads the project and what is running from /api/now, and the
// repository from the Share panel's capabilities.
const apiGet = vi.hoisted(() => vi.fn(() => new Promise(() => {})))
vi.mock("@/lib/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/api")>()),
  apiClient: { get: apiGet },
}))
vi.mock("@/hooks/useTunnelCapabilities", () => ({
  useTunnelCapabilities: () => ({ data: { repo: { remote: "", host: "github.com", slug: "hectorcanaimero/orch", branch: "main" } } }),
}))

function Where() {
  const { pathname, search } = useLocation()
  return <output aria-label="location">{pathname + search}</output>
}

function renderAt(path: string) {
  return render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route
            path="*"
            element={
              <AppLayout>
                <Where />
              </AppLayout>
            }
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

const hrefs = (nav: HTMLElement) => within(nav).getAllByRole("link").map((a) => a.getAttribute("href"))

describe("AppLayout", () => {
  // At 390px a sideways strip of eleven labels cut "Milestones" in half. The
  // phone gets the same four destinations as the sidebar, as a bottom bar.
  // jsdom applies no media queries, so this checks the structure; the
  // breakpoint classes are what switch between the two.
  it("offers the same four destinations in the sidebar and the phone's bottom bar", () => {
    renderAt("/")
    const sidebar = screen.getByRole("navigation", { name: "Main" })
    const bottom = screen.getByRole("navigation", { name: "Main (small screens)" })
    expect(hrefs(sidebar)).toEqual(["/", "/work/list", "/cost/usage", "/delivery/client"])
    expect(hrefs(bottom)).toEqual(hrefs(sidebar))
    // System's pages sit under their own heading, not among the places.
    expect(hrefs(screen.getByRole("region", { name: "System" }))).toEqual(["/system/providers", "/system/tools", "/system/skills", "/system/knowledge", "/system/logs", "/system/settings"])
    expect(bottom).toHaveClass("md:hidden")
    expect(sidebar.closest("aside")).toHaveClass("hidden", "md:flex")
  })

  it("shows a destination's pages as tabs that keep the filters in the query string", () => {
    renderAt("/work/kanban?status=blocked&logs=1")
    const tabs = screen.getByRole("navigation", { name: "Sections" })
    expect(hrefs(tabs)).toEqual([
      "/work/list?status=blocked",
      "/work/kanban?status=blocked",
      "/work/graph?status=blocked",
      "/work/phases?status=blocked",
      "/work/pace?status=blocked",
    ])
    expect(within(tabs).getByRole("link", { current: "page" })).toHaveTextContent("Kanban")
  })

  // #364: Now holds What's new, which carries a dot until the running
  // version's notes have been opened in this browser.
  it("marks What's new until the running version's notes were read", () => {
    window.localStorage.clear()
    const { unmount } = renderAt("/")
    const tabs = screen.getByRole("navigation", { name: "Sections" })
    expect(hrefs(tabs)).toEqual(["/", "/now/releases"])
    expect(within(tabs).getByRole("img", { name: "New notes" })).toBeInTheDocument()
    unmount()

    window.localStorage.setItem("orch.releases.seen", "v0.17.0")
    renderAt("/")
    expect(within(screen.getByRole("navigation", { name: "Sections" })).queryByRole("img", { name: "New notes" })).toBeNull()
  })

  // The phone's bottom bar holds the destinations only; the System group,
  // Share and the session controls live behind "More".
  it("puts System, Share and the session controls behind More on a phone", () => {
    renderAt("/work/list")
    const bottom = screen.getByRole("navigation", { name: "Main (small screens)" })
    fireEvent.click(within(bottom).getByRole("button", { name: "More" }))
    const more = screen.getByRole("dialog")
    expect(within(more).getByText("System")).toBeInTheDocument()
    expect(within(more).getByRole("link", { name: "Tools" })).toHaveAttribute("href", "/system/tools")
    expect(within(more).getByRole("button", { name: "Share" })).toBeInTheDocument()
    expect(within(more).getByRole("button", { name: "Log out" })).toBeInTheDocument()
    fireEvent.click(within(more).getByRole("link", { name: "Logs" }))
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument()
    expect(screen.getByRole("status", { name: "location" })).toHaveTextContent("/system/logs")
  })

  it("collapses and expands the sidebar with [", () => {
    window.localStorage.clear()
    renderAt("/")
    const sidebar = screen.getByRole("complementary")
    expect(sidebar).toHaveClass("w-60")
    fireEvent.keyDown(window, { key: "[" })
    expect(sidebar).toHaveClass("w-14")
    expect(within(sidebar).getByRole("button", { name: "Expand sidebar" })).toHaveAttribute("aria-expanded", "false")
    fireEvent.keyDown(window, { key: "[" })
    expect(sidebar).toHaveClass("w-60")
  })

  // The approved shell: the project and its repository at the top, what is in
  // flight on Now, and a live-run pill in the page header while a run is on.
  it("names the project and repository and shows what is running", async () => {
    apiGet.mockResolvedValueOnce({ data: { project: "acme-billing-api", run: { status: "live" }, working: [{}, {}, {}] } })
    renderAt("/work/list")
    const sidebar = screen.getByRole("complementary")
    expect(await within(sidebar).findByText("acme-billing-api")).toBeInTheDocument()
    expect(within(sidebar).getByText("hectorcanaimero/orch")).toBeInTheDocument()
    expect(within(screen.getByRole("navigation", { name: "Main" })).getByText("3 in flight")).toBeInTheDocument()
    expect(screen.getByText("run active")).toBeInTheDocument()
    expect(within(sidebar).getByText(/Operator · /)).toBeInTheDocument()
  })

  // The design's badge on Tools: how many tools this project uses are missing.
  it("counts the missing tools on System › Tools", async () => {
    apiGet.mockImplementation(((url: string) =>
      url === "/api/tools/missing" ? Promise.resolve({ data: { missing: 2 } }) : new Promise(() => {})) as never)
    renderAt("/work/list")
    const tools = within(screen.getByRole("region", { name: "System" })).getByRole("link", { name: /Tools/ })
    expect(await within(tools).findByText("2")).toBeInTheDocument()
    apiGet.mockImplementation(() => new Promise(() => {}))
  })

  // A client's session says what it is, as the design's sidebar does.
  it("tells a client the view is read-only", () => {
    whoami = { profile: "stakeholder", routes: ["stakeholder_summary_json", "stakeholder_snapshot_json", "api_whoami", "spa"] }
    try {
      renderAt("/delivery/client")
      expect(within(screen.getByRole("complementary")).getByText("Client view · read-only.")).toBeInTheDocument()
    } finally {
      whoami = { profile: "operator" }
    }
  })

  // The event log is a page of its own under System, not a panel over the page.
  it("opens the event log as a System page", () => {
    renderAt("/cost/budget")
    fireEvent.click(within(screen.getByRole("region", { name: "System" })).getByRole("link", { name: "Logs" }))
    expect(screen.getByRole("status", { name: "location" })).toHaveTextContent("/system/logs")
  })
})
