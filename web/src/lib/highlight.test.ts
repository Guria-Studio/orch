import { describe, expect, it } from "vitest"
import { highlightLine, languageOf } from "@/lib/highlight"

describe("languageOf", () => {
  // #454: a diff's lines are colored by their file's extension.
  it("maps a file's extension to a registered language", () => {
    expect(languageOf("internal/app.go")).toBe("go")
    expect(languageOf("web/src/Page.TSX")).toBe("typescript")
    expect(languageOf("scripts/x.mjs")).toBe("javascript")
    expect(languageOf(".github/workflows/go.yml")).toBe("yaml")
    expect(languageOf("README.md")).toBe("")
    expect(languageOf("Makefile")).toBe("")
  })
})

describe("highlightLine", () => {
  it("colors a line and escapes it", () => {
    const html = highlightLine('func A() { return "<b>" }', "go")
    expect(html).toContain('<span class="hljs-keyword">func</span>')
    expect(html).toContain("&lt;b&gt;")
    expect(html).not.toContain("<b>")
  })

  it("leaves a line of an unknown language to plain text", () => {
    expect(highlightLine("# notes", "")).toBeNull()
    expect(highlightLine("x", "cobol")).toBeNull()
  })
})
