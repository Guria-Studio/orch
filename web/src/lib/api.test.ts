import { afterEach, describe, expect, it, vi } from "vitest"
import { addTask, apiClient, editTask, getPortfolio, moveTask, noteTask, PortfolioShapeError, stopTask } from "@/lib/api"
import type { Portfolio } from "@/lib/types"

// The agreed shape (coordination note in
// docs/brainstorm/go-migration-notes/sonnet-2.md), covering the three
// cases orch-98 asked this test to exercise: a normal available row, a
// row whose own spend is hidden (spend.available: false), and the
// separate unavailable[] list with its reason.
const SAMPLE_PORTFOLIO: Portfolio = {
  generated_at: "2026-09-12T04:40:00Z",
  projects: [
    {
      project_id: "billing-api",
      project_name: "billing-api",
      root: "/srv/orch-projects/billing-api",
      available: true,
      total: 12,
      done: 5,
      in_progress: 1,
      blocked: 2,
      backlog: 4,
      percent_done: 41.7,
      velocity_per_day: 1.43,
      eta_days: 3.5,
      eta_date: "2026-09-15",
      confidence: "low",
      blockers: [{ task_id: "F1.T2", title: "Stripe webhook", reason: "missing sandbox key" }],
      spend: { available: true, total_cost_usd: 12.37 },
      last_event: { event_type: "sprint_done", ts: "2026-09-12T04:06:48Z", task_id: "" },
    },
    {
      project_id: "mobile-app",
      project_name: "mobile-app",
      root: "/srv/orch-projects/mobile-app",
      available: true,
      total: 40,
      done: 6,
      in_progress: 3,
      blocked: 5,
      backlog: 26,
      percent_done: 15,
      velocity_per_day: 0.4,
      eta_days: 85,
      eta_date: "2026-12-05",
      confidence: "low",
      blockers: [],
      spend: { available: false },
      last_event: { event_type: "budget_pause", ts: "2026-09-12T02:15:00Z", task_id: "" },
    },
  ],
  unavailable: [{ root: "/srv/orch-projects/legacy", reason: "no tasks.json" }],
}

describe("getPortfolio", () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it("returns the response body as-is, spend.available: false and unavailable[] included", async () => {
    const spy = vi.spyOn(apiClient, "get").mockResolvedValueOnce({ data: SAMPLE_PORTFOLIO })

    const got = await getPortfolio()

    expect(spy).toHaveBeenCalledWith("/api/portfolio")
    expect(got.projects).toHaveLength(2)
    expect(got.projects[0].spend).toEqual({ available: true, total_cost_usd: 12.37 })
    // The row this test exists to check: hidden spend has no total_cost_usd
    // at all, not a null someone could mistake for $0.
    expect(got.projects[1].spend).toEqual({ available: false })
    expect(got.projects[1].spend.total_cost_usd).toBeUndefined()
    expect(got.unavailable).toEqual([{ root: "/srv/orch-projects/legacy", reason: "no tasks.json" }])
  })

  // A single-project dashboard has no /api/portfolio route registered at
  // all (until #201 adds the 404), so the request falls through to the
  // SPA's own catch-all and axios sees 200 with the HTML shell as the
  // body — a real response, just not this one's shape. Caught by opus's
  // review of this PR: without this check, `getPortfolio` would resolve
  // with a string where a `Portfolio` was promised.
  it("rejects with PortfolioShapeError when the body isn't a portfolio (e.g. the SPA's HTML shell on 200)", async () => {
    vi.spyOn(apiClient, "get").mockResolvedValueOnce({ data: "<!doctype html><html>...</html>" })

    await expect(getPortfolio()).rejects.toBeInstanceOf(PortfolioShapeError)
  })

  it("rejects with PortfolioShapeError when `projects` is missing entirely", async () => {
    vi.spyOn(apiClient, "get").mockResolvedValueOnce({ data: { generated_at: "now" } })

    await expect(getPortfolio()).rejects.toBeInstanceOf(PortfolioShapeError)
  })
})

describe("stopTask / noteTask", () => {
  afterEach(() => vi.restoreAllMocks())

  it("posts the body to the task's route, id encoded", async () => {
    const post = vi.spyOn(apiClient, "post").mockResolvedValue({ data: { message: "ok" } })
    await stopTask("F1/T2", true, "wait")
    expect(post).toHaveBeenCalledWith("/api/task/F1%2FT2/stop", { hold: true, note: "wait" })
    await noteTask("F1.T2", "use uv")
    expect(post).toHaveBeenLastCalledWith("/api/task/F1.T2/note", { note: "use uv" })
  })

  it("rejects with the server's detail", async () => {
    vi.spyOn(apiClient, "post").mockRejectedValue({ response: { data: { detail: "loopback only" } } })
    await expect(stopTask("F1.T2", false, "")).rejects.toThrow("loopback only")
  })
})

describe("addTask / editTask / moveTask", () => {
  afterEach(() => vi.restoreAllMocks())

  it("sends each to its route, id encoded", async () => {
    const request = vi.spyOn(apiClient, "request").mockResolvedValue({ data: {} })
    await addTask({ id: "T-9", title: "Docs", model: "claude/sonnet", backlog: true })
    expect(request).toHaveBeenCalledWith({
      method: "post",
      url: "/api/tasks",
      data: { id: "T-9", title: "Docs", model: "claude/sonnet", backlog: true },
    })
    await editTask("F1/T2", { title: "Schema" })
    expect(request).toHaveBeenLastCalledWith({ method: "patch", url: "/api/task/F1%2FT2", data: { title: "Schema" } })
    await moveTask("F1.T2", "blocked", "no key")
    expect(request).toHaveBeenLastCalledWith({
      method: "post",
      url: "/api/task/F1.T2/status",
      data: { status: "blocked", note: "no key" },
    })
  })

  it("rejects with the server's detail", async () => {
    vi.spyOn(apiClient, "request").mockRejectedValue({ response: { data: { detail: "a cycle" } } })
    await expect(editTask("F1.T2", { dependencies: ["F1.T3"] })).rejects.toThrow("a cycle")
  })
})
