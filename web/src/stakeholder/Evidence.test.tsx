import "@testing-library/jest-dom/vitest"
import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { DeliverableEvidence } from "@/stakeholder/Evidence"
import { mediaURL } from "@/stakeholder/portal"

const src = (search: string) => (p: string) => mediaURL(p, search, null)

describe("DeliverableEvidence (#520)", () => {
  it("says how it was checked and shows the walk-through's steps and video", () => {
    const { container } = render(
      <DeliverableEvidence
        evidence={{ checked: ["tests", "e2e"], reviewed: true, e2e: { passed: 2, total: 2 }, steps: ["Abre la tienda", "Paga con tarjeta"], video: "evidence/ab12/video.webm", poster: "evidence/ab12/poster.png" }}
        lang="es"
        mediaSrc={src("?token=tok")}
      />,
    )
    expect(container).toHaveTextContent("Verificado: pruebas automáticas, 2 de 2 recorridos pasaron. Revisado por un segundo modelo.")
    expect(screen.getByText("Cómo se probó")).toBeInTheDocument()
    expect(screen.getByText("Paga con tarjeta")).toBeInTheDocument()
    const video = container.querySelector("video")
    expect(video?.getAttribute("src")).toBe("./evidence/ab12/video.webm?token=tok")
    expect(video?.getAttribute("poster")).toBe("./evidence/ab12/poster.png?token=tok")
    expect(video).toHaveAttribute("aria-label", "Video del recorrido")
  })

  it("shows no video without one, and nothing at all with nothing to say", () => {
    const { container, rerender } = render(<DeliverableEvidence evidence={{ checked: ["build"], reviewed: false, steps: ["Abre"] }} lang="en" mediaSrc={src("")} />)
    expect(container.querySelector("video")).toBeNull()
    expect(container).toHaveTextContent("Checked: a full build.")
    rerender(<DeliverableEvidence evidence={{ reviewed: false }} lang="en" mediaSrc={src("")} />)
    expect(container).toBeEmptyDOMElement()
  })
})
