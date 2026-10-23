import "@testing-library/jest-dom/vitest"
import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"
import { Markdown } from "@/components/Markdown"

const description = [
  "Read `apps/api/src/llm/config.ts` first.",
  "",
  "```go",
  "func main() { return }",
  "```",
  "",
  "```bash",
  "pnpm --filter api test",
  "```",
  "",
  "```",
  "plain block",
  "```",
].join("\n")

describe("Markdown", () => {
  // #428: inline code is a chip; a tagged fence is highlighted, labelled and
  // copyable; an untagged one is monospace without colors.
  it("renders inline code and highlights fenced blocks with a label and a copy button", async () => {
    const { container } = render(<Markdown source={description} />)
    expect(screen.getByText("apps/api/src/llm/config.ts").tagName).toBe("CODE")
    expect(screen.getByText("go")).toBeInTheDocument()
    expect(screen.getByText("bash")).toBeInTheDocument()
    expect(screen.getAllByRole("button", { name: "Copy" })).toHaveLength(3)
    await waitFor(() => expect(container.querySelector("code.language-go .hljs-keyword")).toHaveTextContent("func"))
    expect(container.querySelector("code.language-bash")).toHaveClass("hljs")
    expect(screen.getByText("plain block")).not.toHaveClass("hljs")
  })

  it("copies a block's code", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.assign(navigator, { clipboard: { writeText } })
    render(<Markdown source={"```bash\necho hi\n```"} />)
    fireEvent.click(screen.getByRole("button", { name: "Copy" }))
    expect(writeText).toHaveBeenCalledWith("echo hi\n")
    expect(await screen.findByRole("button", { name: "Copied" })).toBeInTheDocument()
  })

  // The description is agent-written text: nothing executable reaches the DOM.
  it("renders <script> and onerror= inert", () => {
    const { container } = render(<Markdown source={'<script>window.pwned = 1</script>\n\n<img src=x onerror="window.pwned = 1">'} />)
    expect(container.querySelector("script")).toBeNull()
    expect(container.querySelector("img")).not.toHaveAttribute("onerror")
    expect(container.innerHTML).not.toMatch(/onerror/i)
  })

  // A description without markdown keeps its line breaks, as the plain <p> did.
  it("keeps the line breaks of plain text", () => {
    const { container } = render(<Markdown source={"first line\nsecond line"} breaks />)
    expect(container.querySelector("br")).not.toBeNull()
  })
})
