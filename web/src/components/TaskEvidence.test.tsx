import "@testing-library/jest-dom/vitest"
import { render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { setLanguage } from "@/i18n"
import { englishLeaks, visibleText } from "@/i18n/leaks"
import type { Evidence } from "@/lib/evidence"
import { TaskEvidence } from "@/components/TaskEvidence"

const state: { data: Evidence | undefined } = { data: undefined }
vi.mock("@/hooks/useTests", () => ({
  useTaskEvidence: () => ({ data: state.data, isError: false }),
}))

// The shape internal/evidence writes (evidence.json, version 1).
const manifest: Evidence = {
  version: 1,
  task_id: "F2.T3",
  title: "Login",
  run_id: "run-1",
  attempt: 2,
  head_sha: "abc1234def",
  verified_at: "2026-11-20T12:00:00Z",
  verdict: "passed",
  checks: [
    { command: "go test ./...", duration_s: 41.2, kind: "tests" },
    { command: "pnpm exec playwright test", duration_s: 12, kind: "e2e" },
  ],
  review: { reviewer: "gemini", blocking: 0, findings: [{ severity: "minor", title: "naming" }] },
  diff: { files: 7, insertions: 120, deletions: 14, top: ["app.go"] },
  e2e: {
    passed: 5,
    failed: 0,
    flaky: 0,
    skipped: 0,
    dir: "e2e/run-1/F2.T3/attempt-2",
    media: [{ test: "login › entra", kind: "video", path: "t001/video.webm", bytes: 812345 }],
    walkthroughs: [{ title: "login › entra", status: "passed", steps: ["Abre la página", "Entra con su cuenta"], video: "t001/video.webm" }],
  },
}

afterEach(() => {
  setLanguage("en")
  state.data = undefined
})

describe("TaskEvidence", () => {
  it("shows what the local verify proved: checks, review, change, E2E and the walk-through's video", () => {
    state.data = manifest
    render(<TaskEvidence taskId="F2.T3" />)

    const section = screen.getByRole("region", { name: "Evidence" })
    expect(section).toHaveTextContent("Passed · attempt 2 · abc1234")
    expect(section).toHaveTextContent("go test ./...")
    expect(section).toHaveTextContent("41.2s")
    expect(section).toHaveTextContent("Reviewed by gemini: no blocking findings · 1 minor")
    expect(section).toHaveTextContent("7 files, +120 −14")
    expect(section).toHaveTextContent("5 passed · 0 failed")
    expect(section).toHaveTextContent("Entra con su cuenta")
    const video = section.querySelector("video")
    expect(video?.getAttribute("src")).toContain("/api/tests/run-1/F2.T3/2/files/t001/video.webm")
  })

  it("says when the review did not run", () => {
    state.data = { ...manifest, verdict: "review_unavailable", review: { blocking: 0, findings: [], unavailable: "no other provider" } }
    render(<TaskEvidence taskId="F2.T3" />)
    expect(screen.getByRole("region", { name: "Evidence" })).toHaveTextContent("Not reviewed: no other provider")
  })

  it("renders nothing for a task without evidence", () => {
    const { container } = render(<TaskEvidence taskId="F2.T3" />)
    expect(container).toBeEmptyDOMElement()
  })

  it.each(["es", "pt"] as const)("has no English left in %s", (lang) => {
    setLanguage(lang)
    state.data = manifest
    const { container } = render(<TaskEvidence taskId="F2.T3" />)
    expect(englishLeaks(visibleText(container), lang)).toEqual([])
  })
})
