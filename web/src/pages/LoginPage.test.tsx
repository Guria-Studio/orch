import "@testing-library/jest-dom/vitest"
import { render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { describe, expect, it, vi } from "vitest"
import { LoginPage } from "@/pages/LoginPage"

vi.mock("@/hooks/useTunnelCapabilities", () => ({
  useTunnelCapabilities: () => ({ data: { repo: { remote: "", host: "github.com", slug: "Guria-Studio/orch", branch: "main" } } }),
}))

describe("LoginPage", () => {
  // The design's footer names the repository beside the address, when the
  // server tells this machine's operator which one it serves.
  it("names the repository and the address", () => {
    render(
      <MemoryRouter>
        <LoginPage />
      </MemoryRouter>,
    )
    expect(screen.getByText(`Guria-Studio/orch · ${window.location.origin}`)).toBeInTheDocument()
  })
})
