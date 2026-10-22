import "@testing-library/jest-dom/vitest"
import { fireEvent, render, screen, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { setLanguage } from "@/i18n"
import { englishLeaks, visibleText } from "@/i18n/leaks"
import type { E2EReport, E2ERun, TestsResponse } from "@/lib/tests"
import { TestsPage } from "@/pages/TestsPage"

const state: { runs: TestsResponse | undefined; reports: Record<string, E2EReport> } = { runs: undefined, reports: {} }
vi.mock("@/hooks/useTests", () => ({
  useTestRuns: () => ({ data: state.runs, isLoading: false, isError: false, error: null }),
  useTestReport: (run: E2ERun | undefined) => ({
    data: run ? state.reports[`${run.task_id}#${run.attempt}`] : undefined,
    isError: false,
    error: null,
  }),
}))

const run = (attempt: number, passed: number, failed: number): E2ERun => ({
  run_id: "run-1", task_id: "F1.T1", attempt, at: new Date().toISOString(), passed, failed, flaky: 0, skipped: 0,
})

// The shape internal/verify writes from the real Playwright capture.
const failingReport: E2EReport = {
  task_id: "F1.T1",
  started_at: "2026-10-22T20:11:00Z",
  duration_ms: 2390,
  app_url: "http://localhost:3002",
  stats: { passed: 1, failed: 1, flaky: 0, skipped: 0 },
  tests: [
    { title: "checkout › shows the cart", file: "shop.spec.ts", line: 4, status: "passed", duration_ms: 388, retries: 0 },
    {
      title: "checkout › pays with a card", file: "shop.spec.ts", line: 8, status: "failed", duration_ms: 1186, retries: 0,
      error: 'Expected: "Pago aprobado"\nReceived: "Error 500"',
      location: "tests/shop.spec.ts:13",
      steps: ["click Pagar"],
      attachments: [
        { name: "screenshot", content_type: "image/png", path: "t002/test-failed-1.png", source: "test-results/x/test-failed-1.png" },
        { name: "video", content_type: "video/webm", path: "t002/video.webm", source: "test-results/x/video.webm" },
        { name: "trace", content_type: "application/zip", path: "t002/trace.zip", source: "test-results/x/trace.zip" },
      ],
    },
  ],
}

beforeEach(() => {
  window.localStorage.clear()
  window.localStorage.setItem("orch_token", "tok en")
  state.runs = { runs: [run(2, 2, 0), run(1, 1, 1)] }
  state.reports = {
    "F1.T1#1": failingReport,
    "F1.T1#2": { ...failingReport, app_url: undefined, stats: { passed: 2, failed: 0, flaky: 0, skipped: 0 },
      tests: failingReport.tests.map((t) => ({ ...t, status: "passed" as const, error: undefined })) },
  }
})

afterEach(() => setLanguage("en"))

describe("TestsPage", () => {
  it("opens on the newest run and shows a failing test's evidence when picked", () => {
    render(<TestsPage />)
    expect(screen.getByTestId("run-F1.T1-2")).toHaveAttribute("aria-current", "true")
    expect(screen.queryByText(/Received: "Error 500"/)).toBeNull()

    fireEvent.click(screen.getByTestId("run-F1.T1-1"))
    expect(screen.getByText("Against the app at http://localhost:3002")).toBeInTheDocument()

    const failed = screen.getByTestId("test-checkout › pays with a card")
    // Failing tests come first, open.
    expect(screen.getAllByTestId(/^test-/)[0]).toBe(failed)
    expect(failed.querySelector("details")).toHaveAttribute("open")
    expect(within(failed).getByText("tests/shop.spec.ts:13")).toBeInTheDocument()
    expect(within(failed).getByText("click Pagar")).toBeInTheDocument()
    expect(within(failed).getByText(/Received: "Error 500"/)).toBeInTheDocument()

    // Files load with the token in the query: an <img> sends no header.
    const img = within(failed).getByRole("img", { name: /Screenshot/ })
    expect(img.getAttribute("src")).toMatch(/\/api\/tests\/run-1\/F1\.T1\/1\/files\/t002\/test-failed-1\.png\?token=tok\+en$/)
    expect(failed.querySelector("video")?.getAttribute("src")).toMatch(/files\/t002\/video\.webm\?token=/)
    expect(within(failed).getByRole("link", { name: /Download the trace/ }).getAttribute("href")).toMatch(/trace\.zip\?token=tok\+en&download=1$/)
    expect(within(failed).getByText("npx playwright show-trace test-results/x/trace.zip")).toBeInTheDocument()

    const passed = screen.getByTestId("test-checkout › shows the cart")
    expect(passed.querySelector("details")).not.toHaveAttribute("open")
  })

  it("says how to turn E2E on when there is nothing yet", () => {
    state.runs = { runs: [] }
    render(<TestsPage />)
    expect(screen.getByText("No E2E runs yet")).toBeInTheDocument()
    expect(screen.getByText(/e2e_report: e2e-report\.json/)).toBeInTheDocument()
  })

  it("agrees the counts with their number in Spanish", () => {
    setLanguage("es")
    render(<TestsPage />)
    expect(within(screen.getByTestId("run-F1.T1-1")).getByText("1 pasó · 1 falló")).toBeInTheDocument()
    expect(within(screen.getByTestId("run-F1.T1-2")).getByText("2 pasaron · 0 fallaron")).toBeInTheDocument()
  })

  it.each(["es", "pt"] as const)("leaves no English behind in %s", (lang) => {
    setLanguage(lang)
    const { container } = render(<TestsPage />)
    fireEvent.click(screen.getByTestId("run-F1.T1-1"))
    expect(englishLeaks(visibleText(container), lang)).toEqual([])
  })
})
