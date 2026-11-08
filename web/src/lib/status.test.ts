import { describe, expect, it } from "vitest"
import { t } from "@/i18n"
import { criticalPathMatters, STATUS, statusBadgeVariant, statusKey, statusMeta } from "@/lib/status"

// The statuses internal/model/status.go defines, spelled as the API sends them.
const BACKEND_STATUSES = ["backlog", "todo", "in-progress", "done", "blocked"]

describe("status scale", () => {
  it.each(BACKEND_STATUSES)("maps %s to a token, an icon and a label", (status) => {
    const meta = statusMeta(status)
    expect(meta.text).toMatch(/^text-status-/)
    expect(meta.fill).toMatch(/^bg-status-/)
    expect(meta.icon).toBeTruthy()
    const label = t(meta.label)
    expect(label).not.toBe(meta.label)
    expect(label.length).toBeGreaterThan(0)
  })

  it("reads in-progress with either spelling", () => {
    expect(statusKey("in-progress")).toBe("in_progress")
    expect(statusKey("in_progress")).toBe("in_progress")
  })

  // #533: a status the UI does not know used to read as backlog, hiding it.
  // It says it is unknown; only a missing status is backlog.
  it("shows a status it does not know as unknown, not as backlog", () => {
    expect(statusKey("archived")).toBe("unknown")
    expect(t(statusMeta("archived").label)).toBe("Unknown status")
    expect(statusBadgeVariant("archived")).toBe("danger")
    expect(statusMeta("")).toBe(STATUS.backlog)
  })

  // #533: the CLI has five statuses and never sends "failed"; the UI offered
  // it in the list, the task's select and the command palette anyway.
  it("has no failed status the backend cannot produce", () => {
    expect(Object.keys(STATUS)).not.toContain("failed")
    expect(statusKey("failed")).toBe("unknown")
  })

  // Blocked waits on something; unknown is wrong. They must never share a color.
  it("keeps blocked apart from unknown", () => {
    expect(STATUS.blocked.text).not.toBe(STATUS.unknown.text)
    expect(statusBadgeVariant("blocked")).not.toBe("danger")
  })

  it("marks the critical path only when some task is off it", () => {
    expect(criticalPathMatters([{ on_critical_path: true }, { on_critical_path: true }])).toBe(false)
    expect(criticalPathMatters([{ on_critical_path: true }, { on_critical_path: false }])).toBe(true)
    expect(criticalPathMatters([{ on_critical_path: false }])).toBe(false)
  })
})
