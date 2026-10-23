// @vitest-environment node
import { existsSync, readdirSync } from "node:fs"
import { fileURLToPath } from "node:url"
import { describe, expect, it } from "vitest"
import { PIPELINE } from "@/lib/skillsPipeline"

// The skills orch embeds: every internal/skills/<name>/SKILL.md (skills.go's
// go:embed pattern).
const dir = fileURLToPath(new URL("../../../internal/skills", import.meta.url))
const embedded = readdirSync(dir, { withFileTypes: true })
  .filter((d) => d.isDirectory() && existsSync(`${dir}/${d.name}/SKILL.md`))
  .map((d) => d.name)
  .sort()

describe("PIPELINE", () => {
  // #427: the "How it works" tab must not drift from the skills. Adding,
  // renaming or removing a skill fails here until the tab names it.
  it("names every embedded skill exactly once, and no other", () => {
    expect(embedded.length).toBeGreaterThan(0)
    expect(PIPELINE.flatMap((step) => step.skills).sort()).toEqual(embedded)
  })
})
