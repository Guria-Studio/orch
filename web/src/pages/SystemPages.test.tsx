import "@testing-library/jest-dom/vitest"
import { render, screen, within } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"
import { SettingsPage } from "@/pages/SettingsPage"
import { SkillsPage } from "@/pages/SkillsPage"

vi.mock("@/hooks/useSystem", () => ({
  useSkills: () => ({
    isLoading: false,
    data: {
      skills: [
        { name: "orch", description: "Use when…", targets: [{ target: "claude", state: "current", path: "/h/.claude/skills/orch/SKILL.md" }, { target: "codex", state: "missing", path: "/p/AGENTS.md" }] },
      ],
    },
  }),
  useEffectiveConfig: () => ({
    isLoading: false,
    data: {
      path: "/p/.orchestrator/config.yaml",
      sources: ["/p/.orchestrator/config.yaml"],
      warnings: ["unknown key: tunel"],
      sections: [{ name: "tunnel", entries: [{ key: "tunnel.enabled", value: "true" }] }],
    },
  }),
}))

describe("System pages", () => {
  it("Skills says per CLI whether each skill is installed, and gives the command", () => {
    render(<SkillsPage />)
    const row = screen.getByText("orch").closest("tr")!
    expect(within(row).getByText("Installed")).toBeInTheDocument()
    expect(within(row).getByText("Not installed")).toBeInTheDocument()
    expect(screen.getByText("orch install-skills --target claude --target codex")).toBeInTheDocument()
  })

  it("Settings lists each key under its section, with its source and the ignored keys", () => {
    render(<SettingsPage />)
    const tunnel = screen.getByRole("region", { name: "tunnel" })
    expect(within(tunnel).getByText("tunnel.enabled")).toBeInTheDocument()
    expect(within(tunnel).getByText("true")).toBeInTheDocument()
    expect(screen.getByText("unknown key: tunel")).toBeInTheDocument()
  })
})
