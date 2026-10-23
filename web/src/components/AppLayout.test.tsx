import "@testing-library/jest-dom/vitest"
import { fireEvent, render, screen, within } from "@testing-library/react"
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom"
import { describe, expect, it, vi } from "vitest"
import { AppLayout } from "@/components/AppLayout"

vi.mock("@/hooks/useWhoami", () => ({ useWhoami: () => ({ data: { profile: "operator" } }) }))
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

function Where() {
  const { pathname, search } = useLocation()
  return <output aria-label="location">{pathname + search}</output>
}

function renderAt(path: string) {
  return render(
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
    </MemoryRouter>,
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
    expect(hrefs(sidebar)).toEqual(["/", "/work/list", "/cost/budget", "/delivery/client"])
    expect(hrefs(bottom)).toEqual(hrefs(sidebar))
    // System's pages sit under their own heading, not among the places.
    expect(hrefs(screen.getByRole("region", { name: "System" }))).toEqual(["/system/tools"])
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
    fireEvent.click(within(more).getByRole("button", { name: "Logs" }))
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument()
    expect(screen.getByRole("status", { name: "location" })).toHaveTextContent("/work/list?logs=1")
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

  it("opens the event log over the current page and closes it again", () => {
    renderAt("/cost/budget")
    expect(screen.queryByText("event log")).not.toBeInTheDocument()
    fireEvent.click(within(screen.getByRole("complementary")).getByRole("button", { name: "Logs" }))
    expect(screen.getByRole("region", { name: "Logs" })).toHaveTextContent("event log")
    expect(screen.getByRole("status", { name: "location" })).toHaveTextContent("/cost/budget?logs=1")
    fireEvent.click(screen.getByRole("button", { name: "Close logs" }))
    expect(screen.queryByText("event log")).not.toBeInTheDocument()
  })
})
