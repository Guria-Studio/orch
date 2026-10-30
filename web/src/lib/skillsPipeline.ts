// System › Skills › How it works: the path from an idea to dispatched tasks,
// one step per stage and the skills that drive it. skillsPipeline.test.ts
// checks the skills named here against internal/skills/*/SKILL.md, so a skill
// added, renamed or removed fails the tests until this list follows.
export interface PipelineStep {
  key: "prd" | "arch" | "spec" | "tasks" | "run"
  skills: string[]
  /** What the step reads; absent for the first, which starts from an idea. */
  reads?: string
  /** What it writes; absent for the run, which writes code and PRs. */
  writes?: string
  command: string
}

export const PIPELINE: PipelineStep[] = [
  { key: "prd", skills: ["orch-plan", "orch-prd"], writes: "docs/prd/NNN-<slug>.md", command: "/orch-plan <idea>" },
  { key: "arch", skills: ["orch-discover", "orch-arch"], reads: "docs/prd/NNN-<slug>.md", writes: "docs/arch/NNN-<slug>.md", command: "/orch-arch" },
  { key: "spec", skills: ["orch-spec"], reads: "docs/arch/NNN-<slug>.md", writes: "specs/fN-<slug>.md", command: "/orch-spec" },
  { key: "tasks", skills: ["orch-tasks"], reads: "specs/fN-<slug>.md", writes: "tasks.json", command: "/orch-tasks" },
  { key: "run", skills: ["orch-tdd", "orch-e2e", "orch"], reads: "tasks.json", command: "orch run" },
]
