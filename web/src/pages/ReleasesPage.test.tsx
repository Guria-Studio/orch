import "@testing-library/jest-dom/vitest"
import { render, screen, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import type { ReleasesPayload } from "@/hooks/useReleases"
import { setLanguage } from "@/i18n"
import { englishLeaks, visibleText } from "@/i18n/leaks"
import { ReleasesPage } from "@/pages/ReleasesPage"

const state: { data: ReleasesPayload | undefined } = { data: undefined }
vi.mock("@/hooks/useReleases", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/hooks/useReleases")>()),
  useReleases: () => ({ data: state.data, isLoading: false, isError: false, error: null }),
}))

const release = (tag: string, extra: Partial<ReleasesPayload["releases"][number]> = {}) => ({
  tag, url: `https://github.com/Guria-Studio/orch/releases/tag/${tag}`, critical: false,
  notes: `- change in ${tag}`, published_at: "2026-10-16T12:00:00Z", newer: false, current: false, ...extra,
})

beforeEach(() => {
  window.localStorage.clear()
  state.data = {
    current: "v0.17.0",
    releases: [
      release("v0.18.0", { newer: true, critical: true, notes: "- **steer** a task <img src=x onerror=alert(1)>\n- [docs](https://x.dev)" }),
      release("v0.17.0", { current: true }),
      release("v0.16.0", { notes: "" }),
    ],
  }
})

describe("ReleasesPage", () => {
  it("lists each release's notes, marking yours and what upgrading brings", () => {
    render(<ReleasesPage />)
    expect(screen.getByText(/You run v0.17.0\. 1 newer release is available\./)).toBeInTheDocument()
    // The approved design names the upgrade command on the newer release's
    // own chip rather than in a separate command box above the list.
    expect(screen.queryByText("orch upgrade")).toBeNull()

    const newer = screen.getByTestId("release-v0.18.0")
    expect(within(newer).getByText("Available — orch upgrade")).toBeInTheDocument()
    expect(within(newer).getByText("Critical")).toBeInTheDocument()
    expect(within(newer).getByText("steer").tagName).toBe("STRONG")
    expect(within(newer).getByRole("link", { name: "docs" })).toHaveAttribute("href", "https://x.dev")
    // GitHub's notes are data: no handler survives.
    expect(newer.querySelector("[onerror]")).toBeNull()

    expect(within(screen.getByTestId("release-v0.17.0")).getByText("You run this")).toBeInTheDocument()
    expect(within(screen.getByTestId("release-v0.16.0")).getByText("No notes for this release.")).toBeInTheDocument()
    expect(window.localStorage.getItem("orch.releases.seen")).toBe("v0.17.0")
  })

  it("says why when GitHub could not be read, and still shows what it has", () => {
    state.data = { current: "v0.17.0", releases: [release("v0.17.0", { current: true })], error: "HTTP 403" }
    render(<ReleasesPage />)
    expect(screen.getByText(/Could not read the latest releases \(HTTP 403\)/)).toBeInTheDocument()
    expect(screen.getByTestId("release-v0.17.0")).toBeInTheDocument()
    expect(screen.queryByText("orch upgrade")).toBeNull()
  })

  it("says so when there is nothing to list", () => {
    state.data = { current: "dev", releases: [], error: "update checks are off (ORCH_NO_UPDATE_CHECK)" }
    render(<ReleasesPage />)
    expect(screen.getByText("No releases to show yet.")).toBeInTheDocument()
  })
})

describe("ReleasesPage in Portuguese", () => {
  afterEach(() => setLanguage("en"))
  it("leaves no English of its own on the page", () => {
    setLanguage("pt")
    state.data = { current: "v0.17.0", releases: [release("v0.18.0", { newer: true, critical: true, notes: "" }), release("v0.17.0", { current: true, notes: "" })] }
    const { container } = render(<ReleasesPage />)
    expect(screen.getByRole("heading", { name: "Novidades do orch" })).toBeInTheDocument()
    expect(englishLeaks(visibleText(container), "pt")).toEqual([])
  })
})
