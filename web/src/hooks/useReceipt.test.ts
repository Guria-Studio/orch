import { afterEach, describe, expect, it, vi } from "vitest"
import { receiptImage } from "@/hooks/useReceipt"

const get = vi.fn()
vi.mock("@/lib/api", () => ({ apiClient: { get: (...args: unknown[]) => get(...args) } }))

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

// jsdom has no canvas and decodes no image, so both are stand-ins: what is
// checked is the path — this run's card, at twice its size, out as a PNG,
// and the object URL given back.
describe("receiptImage", () => {
  function stubBrowser(blob: Blob | null) {
    const revokeObjectURL = vi.fn()
    vi.stubGlobal("URL", { ...URL, createObjectURL: vi.fn().mockReturnValue("blob:svg"), revokeObjectURL })
    vi.stubGlobal(
      "Image",
      class {
        src = ""
        naturalWidth = 1200
        naturalHeight = 630
        decode() {
          return Promise.resolve()
        }
      },
    )
    const drawImage = vi.fn()
    const canvas = {
      width: 0,
      height: 0,
      getContext: () => ({ drawImage }),
      toBlob: (cb: (b: Blob | null) => void, type: string) => {
        expect(type).toBe("image/png")
        cb(blob)
      },
    }
    const create = document.createElement.bind(document)
    vi.spyOn(document, "createElement").mockImplementation((tag: string) =>
      tag === "canvas" ? (canvas as unknown as HTMLCanvasElement) : create(tag),
    )
    return { canvas, drawImage, revokeObjectURL }
  }

  it("draws this run's card at twice its size and gives back a PNG", async () => {
    get.mockResolvedValue({ data: "<svg/>" })
    const png = new Blob(["png"], { type: "image/png" })
    const { canvas, drawImage, revokeObjectURL } = stubBrowser(png)

    await expect(receiptImage("run-7", false)).resolves.toBe(png)
    expect(get).toHaveBeenCalledWith("/api/receipt.svg", { params: { run: "run-7", built_with: "0" }, responseType: "text" })
    expect([canvas.width, canvas.height]).toEqual([2400, 1260])
    expect(drawImage).toHaveBeenCalledWith(expect.anything(), 0, 0, 2400, 1260)
    expect(revokeObjectURL).toHaveBeenCalledWith("blob:svg")
  })

  it("keeps the mark unless asked, and fails when the canvas gives nothing", async () => {
    get.mockResolvedValue({ data: "<svg/>" })
    const { revokeObjectURL } = stubBrowser(null)
    await expect(receiptImage("run-7", true)).rejects.toThrow(/toBlob/)
    expect(get).toHaveBeenCalledWith("/api/receipt.svg", { params: { run: "run-7" }, responseType: "text" })
    expect(revokeObjectURL).toHaveBeenCalled()
  })
})
