import { describe, expect, it } from "vitest"
import { DESTINATIONS, LEGACY_REDIRECTS, findTab, homePath, isPathAllowed, visibleDestinations } from "@/lib/nav"

// The stakeholder allow-list exactly as the Go server sends it in /api/whoami
// (internal/dashboard/access.go DefaultStakeholderRoutes).
const STAKEHOLDER_ROUTES = [
  "stakeholder_summary_json",
  "stakeholder_snapshot_json",
  "api_tunnel_capabilities",
  "api_whoami",
  "api_docs_list",
  "api_docs_content",
  "spa",
]

const shape = (ds: ReturnType<typeof visibleDestinations>) =>
  Object.fromEntries(ds.map((d) => [d.id, d.tabs.map((tab) => tab.to)]))

describe("visibleDestinations", () => {
  it("gives an operator the four destinations and System, Portfolio only when available", () => {
    const off = visibleDestinations({ isStakeholder: false, portfolioAvailable: false })
    expect(off.map((d) => d.id)).toEqual(["now", "work", "cost", "delivery", "system"])
    expect(shape(off).system).toEqual(["/system/providers", "/system/tools", "/system/skills", "/system/knowledge", "/system/logs", "/system/settings"])
    expect(shape(off).now).toEqual(["/", "/now/releases"])
    expect(shape(visibleDestinations({ isStakeholder: false, portfolioAvailable: true })).now).toEqual([
      "/",
      "/now/portfolio",
      "/now/releases",
    ])
  })

  // The bug this rule exists for: pages offered to a stakeholder token that
  // the server answered 403 on.
  it("shows a stakeholder only the pages whose data route the server allows", () => {
    const ds = visibleDestinations({ isStakeholder: true, allowedRoutes: STAKEHOLDER_ROUTES, portfolioAvailable: false })
    expect(shape(ds)).toEqual({ delivery: ["/delivery/client", "/delivery/summary"] })
    // A client's link lands on what the portal used to show them.
    expect(homePath(ds)).toBe("/delivery/client")
  })

  it("follows a wider allow-list when the server grants one", () => {
    const ds = visibleDestinations({
      isStakeholder: true,
      allowedRoutes: [...STAKEHOLDER_ROUTES, "api_tasks", "api_sprint"],
      portfolioAvailable: false,
    })
    expect(shape(ds)).toEqual({
      work: ["/work/list", "/work/kanban", "/work/pace"],
      delivery: ["/delivery/client", "/delivery/summary"],
    })
  })

  it("falls back to hiding operator-only pages when an older server sends no list", () => {
    const tabs = visibleDestinations({ isStakeholder: true, portfolioAvailable: false }).flatMap((d) => d.tabs)
    expect(tabs.map((t) => t.to)).not.toContain("/")
    expect(tabs.map((t) => t.to)).toContain("/delivery/summary")
  })
})

describe("findTab", () => {
  it("matches the Now page only at the root, and a tab by prefix", () => {
    expect(findTab("/")?.destination.id).toBe("now")
    expect(findTab("/work/graph")?.tab.to).toBe("/work/graph")
    expect(findTab("/work/graph/extra")?.tab.to).toBe("/work/graph")
    expect(findTab("/workshop")).toBeUndefined()
  })

  it("files a task's page under the task list, so a stakeholder needs the same route for both", () => {
    expect(findTab("/work/tasks/F6.1.T1")?.tab.to).toBe("/work/list")
    expect(isPathAllowed("/work/tasks/F6.1.T1", { isStakeholder: true, allowedRoutes: ["api_budget_summary"] })).toBe(false)
    expect(isPathAllowed("/work/tasks/F6.1.T1", { isStakeholder: true, allowedRoutes: ["api_tasks"] })).toBe(true)
  })
})

describe("LEGACY_REDIRECTS", () => {
  it("sends every old address to a page that exists", () => {
    const paths = DESTINATIONS.flatMap((d) => d.tabs.map((t) => t.to))
    for (const [from, to] of Object.entries(LEGACY_REDIRECTS)) {
      expect(paths, `${from} → ${to}`).toContain(to)
    }
    expect(Object.keys(LEGACY_REDIRECTS)).toEqual(
      expect.arrayContaining(["/list", "/kanban", "/graph", "/milestones", "/pace", "/sprint", "/budget", "/metrics", "/ci", "/tunnel"]),
    )
  })
})

describe("isPathAllowed", () => {
  it("rejects a page a stakeholder opens by URL when its route is not allowed", () => {
    expect(isPathAllowed("/work/kanban", { isStakeholder: true, allowedRoutes: STAKEHOLDER_ROUTES })).toBe(false)
    expect(isPathAllowed("/", { isStakeholder: true, allowedRoutes: STAKEHOLDER_ROUTES })).toBe(false)
    expect(isPathAllowed("/delivery/summary", { isStakeholder: true, allowedRoutes: STAKEHOLDER_ROUTES })).toBe(true)
  })

  it("never blocks an operator or an unknown path", () => {
    expect(isPathAllowed("/work/kanban", { isStakeholder: false })).toBe(true)
    expect(isPathAllowed("/login", { isStakeholder: true, allowedRoutes: STAKEHOLDER_ROUTES })).toBe(true)
    expect(isPathAllowed("/work/kanban", { isStakeholder: true, allowedRoutes: undefined })).toBe(true)
  })
})
