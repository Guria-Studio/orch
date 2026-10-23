import "@testing-library/jest-dom/vitest"
import { fireEvent, render, screen, within } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"
import { SettingsPage } from "@/pages/SettingsPage"
import { SkillsPage } from "@/pages/SkillsPage"

const save = vi.hoisted(() => ({ mutate: vi.fn(), isPending: false, isError: false, error: null as Error | null }))
const who = vi.hoisted(() => ({ profile: "operator" }))
vi.mock("@/hooks/useWhoami", () => ({ useWhoami: () => ({ data: { profile: who.profile } }) }))
vi.mock("@/hooks/useSystem", () => ({
  useSaveConfig: () => save,
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
      sections: [
        { name: "tunnel", entries: [{ key: "tunnel.enabled", value: "true" }] },
        { name: "concurrency", entries: [{ key: "concurrency.global_max", value: "2" }] },
      ],
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

  // #427: the second tab walks idea → PRD → ARCH → SPEC → TASKS → run, each
  // step with its skill, input, output and command; a skill no CLI has says so.
  it("Skills has a How it works tab with the five steps", () => {
    render(<SkillsPage />)
    expect(screen.getByRole("tab", { name: "Installed" })).toHaveAttribute("aria-selected", "true")
    fireEvent.click(screen.getByRole("tab", { name: "How it works" }))
    const steps = screen.getAllByRole("listitem").filter((li) => li.dataset.step)
    expect(steps.map((li) => li.dataset.step)).toEqual(["prd", "arch", "spec", "tasks", "run"])
    const arch = steps[1]
    expect(within(arch).getByText("orch-arch")).toBeInTheDocument()
    expect(within(arch).getByText("docs/prd/NNN-<slug>.md")).toBeInTheDocument()
    expect(within(arch).getByText("docs/arch/NNN-<slug>.md")).toBeInTheDocument()
    expect(within(arch).getByText("/orch-arch")).toBeInTheDocument()
    // Only orch is installed (in claude); the planning skills are not.
    expect(within(arch).getByText(/Not installed/)).toHaveTextContent("orch install-skills")
    expect(within(steps[4]).getByText("orch")).toHaveAttribute("title", "Installed in claude")
    fireEvent.click(within(arch).getByRole("button", { name: "See Installed" }))
    expect(screen.getByRole("tab", { name: "Installed" })).toHaveAttribute("aria-selected", "true")
  })

  // Anyone but the operator reads the settings: switches and fields are inert.
  it("Settings is read-only for a stakeholder", () => {
    who.profile = "stakeholder"
    render(<SettingsPage />)
    const tunnel = screen.getByRole("region", { name: "Tunnel" })
    expect(within(tunnel).getByText("tunnel.enabled")).toBeInTheDocument()
    const sw = within(tunnel).getByRole("switch", { name: "Allow sharing" })
    expect(sw).toHaveAttribute("aria-checked", "true")
    expect(sw).toBeDisabled()
    expect(screen.getByText("Read-only — edit the file")).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Save" })).not.toBeInTheDocument()
    who.profile = "operator"
  })

  // The operator edits; Save sends only the changed keys, Discard drops them.
  it("Settings saves only what the operator changed", () => {
    save.mutate.mockReset()
    render(<SettingsPage />)
    const saveButton = screen.getByRole("button", { name: "Save" })
    expect(saveButton).toBeDisabled()
    fireEvent.click(screen.getByRole("switch", { name: "Allow sharing" }))
    expect(screen.getByRole("switch", { name: "Allow sharing" })).toHaveAttribute("aria-checked", "false")
    fireEvent.click(screen.getByRole("button", { name: "Discard" }))
    expect(screen.getByRole("switch", { name: "Allow sharing" })).toHaveAttribute("aria-checked", "true")

    fireEvent.click(screen.getByRole("switch", { name: "Allow sharing" }))
    const execution = screen.getByRole("region", { name: "Execution" })
    fireEvent.change(within(execution).getAllByRole("textbox")[0], { target: { value: "4" } })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    expect(save.mutate).toHaveBeenCalledWith({ "tunnel.enabled": "false", "concurrency.global_max": "4" }, expect.anything())
  })

  it("Settings shows why a save was refused", () => {
    save.isError = true
    save.error = new Error("concurrency.global_max: want a whole number, got \"lots\"")
    render(<SettingsPage />)
    expect(screen.getByText(/want a whole number/)).toBeInTheDocument()
    save.isError = false
    save.error = null
  })
})
