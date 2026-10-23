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
  // As in the design: claude, codex and opencode columns, "Up to date" /
  // "Missing" states, and a header button that copies the install command.
  it("Skills says per CLI whether each skill is installed, and copies the command", () => {
    render(<SkillsPage />)
    const row = screen.getByText("orch").closest("tr")!
    expect(within(row).getByText("Up to date")).toBeInTheDocument()
    expect(within(row).getByText("Missing")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Copy the command: install / update all" })).toHaveAttribute(
      "title",
      "orch install-skills --target claude --target codex --target opencode --force",
    )
  })

  // The design's sections with labelled fields; booleans as switches, which
  // are read-only because nothing writes config.yaml from the dashboard.
  it("Settings shows each key in its section, read-only", () => {
    render(<SettingsPage />)
    const tunnel = screen.getByRole("region", { name: "Tunnel" })
    expect(within(tunnel).getByText("tunnel.enabled")).toBeInTheDocument()
    const sw = within(tunnel).getByRole("switch", { name: "Allow sharing" })
    expect(sw).toHaveAttribute("aria-checked", "true")
    expect(sw).toBeDisabled()
    expect(screen.getByText("Read-only — edit the file")).toBeInTheDocument()
  })
})
