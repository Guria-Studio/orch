import type { ReactNode } from "react"
import { AlertTriangle } from "lucide-react"
import { Link } from "react-router-dom"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Skeleton } from "@/components/ui/skeleton"
import { useCI } from "@/hooks/useCI"
import { useTasks } from "@/hooks/useTasks"
import { maxRetries, pipelineStages, prLabel, prTasks, type CiWorkflow, type Stage, type StageState } from "@/lib/ci"
import { describeLoadError } from "@/lib/errors"
import { taskPath } from "@/lib/nav"
import { t } from "@/i18n"
import { cn } from "@/lib/utils"
import type { CiStatus, Task } from "@/lib/types"

const STATE_LABEL: Record<StageState, "ci.stage.on" | "ci.stage.off" | "ci.stage.missing"> = {
  on: "ci.stage.on",
  off: "ci.stage.off",
  missing: "ci.stage.missing",
}

/**
 * CI (operator only): how a task's work gets from its agent to the base
 * branch in this project, as config.yaml and the repo's workflows set it up,
 * and where every pull request is on that path right now.
 */
export function CIPage() {
  const ci = useCI()
  const tasks = useTasks()

  return (
    <div className="flex flex-col gap-6 text-[13px]">
      {/* The layout's header names the page. */}
      <h1 className="sr-only">{t("ci.title")}</h1>
      <div className="flex flex-wrap items-start justify-between gap-x-6 gap-y-2">
        <p className="max-w-[720px] leading-5 text-muted-foreground">{t("ci.intro")}</p>
        {tasks.data ? <PrCountsLine tasks={tasks.data.tasks} /> : null}
      </div>

      {ci.isError ? (
        <Alert variant="destructive">
          <AlertTriangle className="h-4 w-4" />
          <AlertTitle>{t("ci.setup_failed")}</AlertTitle>
          <AlertDescription>{describeLoadError(ci.error)}</AlertDescription>
        </Alert>
      ) : null}

      {ci.data?.warnings.length ? (
        <Alert className="border-status-blocked/40 bg-status-blocked/10 [&>svg]:text-status-blocked">
          <AlertTriangle className="h-4 w-4" />
          <AlertTitle>{t("ci.will_not_work")}</AlertTitle>
          <AlertDescription>
            <ul className="mt-1 list-disc space-y-1 pl-4">
              {ci.data.warnings.map((w) => (
                <li key={w}>{w}</li>
              ))}
            </ul>
          </AlertDescription>
        </Alert>
      ) : null}

      <section aria-labelledby="pipeline-heading" className="flex flex-col gap-2">
        <h2 id="pipeline-heading" className="font-medium">
          {t("ci.pipeline")}
        </h2>
        {ci.isLoading ? (
          <Skeleton className="h-36 w-full" />
        ) : ci.data ? (
          <ol className="grid gap-2 sm:grid-cols-2 lg:grid-cols-5">
            {pipelineStages(ci.data).map((stage, i) => (
              <StageCard key={stage.key} stage={stage} n={i + 1} />
            ))}
          </ol>
        ) : null}
      </section>

      <section aria-labelledby="prs-heading" className="flex flex-col gap-2">
        <h2 id="prs-heading" className="font-medium">
          {t("ci.pull_requests")}
        </h2>
        {tasks.isError ? (
          <Alert variant="destructive">
            <AlertTriangle className="h-4 w-4" />
            <AlertTitle>{t("tasks.load_failed")}</AlertTitle>
            <AlertDescription>{describeLoadError(tasks.error)}</AlertDescription>
          </Alert>
        ) : tasks.isLoading ? (
          <Skeleton className="h-32 w-full" />
        ) : tasks.data ? (
          <PrTable tasks={tasks.data.tasks} retries={ci.data ? maxRetries(ci.data.config) : undefined} />
        ) : null}
      </section>

      <section aria-labelledby="workflows-heading" className="flex flex-col gap-2">
        <h2 id="workflows-heading" className="font-medium">
          {t("ci.workflows")}
        </h2>
        {ci.data && ci.data.workflows.length === 0 ? (
          <p className="rounded-lg border border-dashed p-5 text-muted-foreground">
            {t("ci.no_workflows_before")} <code className="font-mono">.github/workflows</code>.{" "}
            <code className="font-mono">orch init</code> {t("ci.no_workflows_writes")} <code className="font-mono">orch-ci.yml</code>{" "}
            {t("ci.no_workflows_after")}
          </p>
        ) : ci.data ? (
          <ul className="divide-y rounded-lg border">
            {ci.data.workflows.map((w) => (
              <WorkflowRow key={w.file} workflow={w} />
            ))}
          </ul>
        ) : null}
      </section>
    </div>
  )
}

const PILL: Record<StageState | "on_prs" | "off_prs", string> = {
  on: "bg-status-done/14 text-status-done",
  on_prs: "bg-status-done/14 text-status-done",
  off: "bg-secondary text-muted-foreground",
  off_prs: "bg-secondary text-muted-foreground",
  missing: "bg-status-blocked/14 text-status-blocked",
}

function Pill({ tone, children }: { tone: keyof typeof PILL; children: ReactNode }) {
  return <span className={cn("self-start rounded-full px-2 py-px text-[11px]", PILL[tone])}>{children}</span>
}

// The settings each stage reads stay on the card's tooltip; the card says
// what they amount to.
function StageCard({ stage, n }: { stage: Stage; n: number }) {
  return (
    <li
      className="flex flex-col gap-2 rounded-lg border bg-card p-3"
      title={stage.settings.map((s) => `${s.label}: ${s.value}`).join("\n") || undefined}
    >
      <span className="flex items-center gap-2">
        <span className="font-mono text-[11px] text-muted-foreground">{n}</span>
        <span className="font-medium">{stage.title}</span>
      </span>
      <Pill tone={stage.state}>{t(STATE_LABEL[stage.state])}</Pill>
      <span className="text-xs leading-[18px] text-foreground/85">{stage.detail}</span>
    </li>
  )
}

function PrCountsLine({ tasks }: { tasks: Task[] }) {
  const { active, finished, failing } = prTasks(tasks)
  if (active.length + finished.length === 0) return null
  return (
    <p className="text-xs text-muted-foreground tabular-nums">
      {t("ci.counts", { active: active.length, failing, finished: finished.length })}
    </p>
  )
}

const TASK_STATUS: Record<string, "ci.task.in_review" | "ci.task.blocked" | "ci.task.retrying" | "ci.task.done"> = {
  "in-progress": "ci.task.in_review",
  blocked: "ci.task.blocked",
  todo: "ci.task.retrying",
  done: "ci.task.done",
}

const CI_TONE: Record<CiStatus, { label: "ci.pending" | "ci.success" | "ci.failure" | "ci.skipped"; text: string; bg: string }> = {
  pending: { label: "ci.pending", text: "text-status-running", bg: "bg-status-running" },
  success: { label: "ci.success", text: "text-status-done", bg: "bg-status-done" },
  failure: { label: "ci.failure", text: "text-status-blocked", bg: "bg-status-blocked" },
  skipped: { label: "ci.skipped", text: "text-muted-foreground", bg: "bg-muted-foreground" },
}

const PR_COLS = "grid grid-cols-[minmax(0,1fr)_140px_130px_100px_60px] items-center gap-x-3.5 px-3.5"

// One table for every task with a PR: the ones still in review first, then
// the finished ones.
function PrTable({ tasks, retries }: { tasks: Task[]; retries?: number }) {
  const { active, finished } = prTasks(tasks)
  const rows = [...active, ...finished]
  if (rows.length === 0) {
    return (
      <p className="rounded-lg border border-dashed p-5 text-muted-foreground">
        {t("ci.no_prs_before")} <code className="font-mono">vcs.auto_pr</code> {t("ci.no_prs_after")}
      </p>
    )
  }
  return (
    <div className="overflow-x-auto rounded-lg border">
      <div role="table" aria-labelledby="prs-heading" className="min-w-[720px]">
        <div role="row" className={cn(PR_COLS, "h-[34px] border-b bg-card text-xs text-muted-foreground")}>
          <span role="columnheader">{t("ci.col.task")}</span>
          <span role="columnheader">{t("ci.col.task_status")}</span>
          <span role="columnheader">{t("ci.col.last_ci")}</span>
          <span role="columnheader">{t("ci.col.retries")}</span>
          <span role="columnheader">{t("task.pr")}</span>
        </div>
        {rows.map((task) => {
          const tone = CI_TONE[task.ci_status ?? "pending"]
          return (
            <div role="row" key={task.id} className={cn(PR_COLS, "h-10 border-b last:border-b-0")}>
              <Link role="cell" to={taskPath(task.id)} className="flex min-w-0 gap-2.5 hover:underline">
                <span className="font-mono text-xs text-muted-foreground">{task.id}</span>
                <span className="truncate">{task.title}</span>
              </Link>
              <span role="cell" className="text-foreground/85">
                {TASK_STATUS[task.status] ? t(TASK_STATUS[task.status]) : task.status}
              </span>
              <span role="cell" className={cn("inline-flex items-center gap-1.5", tone.text)}>
                <span className={cn("h-1.5 w-1.5 rounded-full", tone.bg)} aria-hidden />
                {t(tone.label)}
              </span>
              <span role="cell" className="font-mono text-muted-foreground tabular-nums">
                {task.ci_attempts ?? 0}
                {retries !== undefined ? ` / ${retries}` : ""}
              </span>
              <a role="cell" href={task.pr_url ?? undefined} target="_blank" rel="noopener noreferrer" className="font-mono text-brand hover:underline">
                {prLabel(task.pr_url ?? "")}
              </a>
            </div>
          )
        })}
      </div>
    </div>
  )
}

// A workflow is one row: its file, whether it runs on PRs, and its jobs
// (with what each waits on).
function WorkflowRow({ workflow }: { workflow: CiWorkflow }) {
  const jobs = workflow.jobs.map((j) => (j.needs.length ? `${j.name || j.id} (${t("ci.after", { jobs: j.needs.join(", ") })})` : j.name || j.id))
  return (
    <li className="flex min-h-11 flex-wrap items-center gap-x-4 gap-y-2 px-3.5 py-2">
      <span className="font-mono text-xs">.github/workflows/{workflow.file}</span>
      <Pill tone={workflow.runs_on_pull_requests ? "on_prs" : "off_prs"}>
        {workflow.runs_on_pull_requests ? t("ci.runs_on_prs") : t("ci.not_on_prs")}
      </Pill>
      {workflow.parse_error ? (
        <span className="text-xs text-status-failed">
          {t("ci.unreadable")}: <span className="font-mono">{workflow.parse_error}</span>
        </span>
      ) : (
        <span className="text-xs text-muted-foreground">{jobs.join(" · ")}</span>
      )}
    </li>
  )
}
