import "@testing-library/jest-dom/vitest"
import { act, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { RunReceipt } from "@/components/RunReceipt"
import { receiptMarkdown, type ReceiptPayload } from "@/hooks/useReceipt"

const receiptImage = vi.fn()
vi.mock("@/hooks/useReceipt", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/hooks/useReceipt")>()),
  receiptImage: (...args: unknown[]) => receiptImage(...args),
}))

const payload: ReceiptPayload = {
  markdown: "## orch run `r` — finished\n",
  built_with: "Built with [orch](https://github.com/hectorcanaimero/orch)",
  receipt: {
    run_id: "r", started_at: "", finished_at: "", finished: true, wall_seconds: 60, agent_seconds: 30,
    dispatches: 1, failed_attempts: 0, retries: 0, done: [], blocked: [], providers: [],
    total_cost_usd: 0, estimated_cost_usd: 0, prs: [],
  },
}

afterEach(() => {
  window.localStorage.clear()
  vi.unstubAllGlobals()
})

describe("RunReceipt", () => {
  it("adds the Built with line unless unticked", () => {
    expect(receiptMarkdown(payload, true)).toBe(`${payload.markdown}\n${payload.built_with}\n`)
    expect(receiptMarkdown(payload, false)).toBe(payload.markdown)
  })

  it("copies the Markdown, remembering the opt-out", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal("navigator", { clipboard: { writeText } })
    render(<RunReceipt payload={payload} />)

    fireEvent.click(screen.getByRole("checkbox", { name: /Built with orch/ }))
    expect(window.localStorage.getItem("orch.receipt.builtWith")).toBe("off")
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Copy as Markdown" }))
    })
    expect(writeText).toHaveBeenCalledWith(payload.markdown)
  })

  it("shows the text to select when the clipboard is refused", async () => {
    vi.stubGlobal("navigator", { clipboard: { writeText: vi.fn().mockRejectedValue(new Error("denied")) } })
    render(<RunReceipt payload={payload} />)
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Copy as Markdown" }))
    })
    expect(screen.getByRole("textbox")).toHaveValue(`${payload.markdown}\n${payload.built_with}\n`)
  })

  it("downloads the card as a PNG of this run, without the mark when unticked", async () => {
    const png = new Blob(["png"], { type: "image/png" })
    receiptImage.mockResolvedValue(png)
    const createObjectURL = vi.fn().mockReturnValue("blob:card")
    vi.stubGlobal("URL", { ...URL, createObjectURL, revokeObjectURL: vi.fn() })
    const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => {})
    render(<RunReceipt payload={payload} />)

    fireEvent.click(screen.getByRole("checkbox", { name: /Built with orch/ }))
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Download image" }))
    })
    expect(receiptImage).toHaveBeenCalledWith("r", false)
    expect(createObjectURL).toHaveBeenCalledWith(png)
    expect(click).toHaveBeenCalled()
    expect((click.mock.contexts[0] as HTMLAnchorElement).download).toBe("orch-r.png")
    click.mockRestore()
  })

  it("copies the card as an image where the clipboard takes one, and says so when it fails", async () => {
    const write = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal("navigator", { clipboard: { write, writeText: vi.fn() } })
    vi.stubGlobal("ClipboardItem", class {
      constructor(public items: Record<string, unknown>) {}
    })
    receiptImage.mockResolvedValue(new Blob(["png"]))
    render(<RunReceipt payload={payload} />)
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Copy image" }))
    })
    expect(write).toHaveBeenCalledTimes(1)
    expect(screen.getByRole("button", { name: "Copied" })).toBeInTheDocument()

    receiptImage.mockRejectedValue(new Error("no canvas"))
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Download image" }))
    })
    expect(screen.getByRole("alert")).toHaveTextContent("Could not make the image")
  })

  it("hides Copy image where the clipboard cannot take one", () => {
    vi.stubGlobal("navigator", { clipboard: { writeText: vi.fn() } })
    render(<RunReceipt payload={payload} />)
    expect(screen.queryByRole("button", { name: "Copy image" })).not.toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Download image" })).toBeInTheDocument()
  })
})
