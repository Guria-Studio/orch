import { StatusBadge } from "@/components/StatusBadge"
import { t } from "@/i18n"
import { cn } from "@/lib/utils"
import type { CiStatus, Task } from "@/lib/types"

const CI: Record<CiStatus, { label: "ci.pending" | "ci.success" | "ci.failure" | "ci.skipped"; className: string }> = {
  pending: { label: "ci.pending", className: "text-status-running" },
  success: { label: "ci.success", className: "text-status-done" },
  failure: { label: "ci.failure", className: "text-status-failed" },
  skipped: { label: "ci.skipped", className: "text-muted-foreground" },
}

export interface TaskCardProps {
  task: Task
  taskStatusMap?: Record<string, string>
  onClick?: (taskId: string) => void
  /** Kanban columns already say the status; the card grid does not. */
  showStatus?: boolean
  /** Only when the project has branches — see criticalPathMatters. */
  showCriticalPath?: boolean
}

/**
 * The one task card: kanban columns and the task list's card view. Id and a
 * marker (ready to start, or on the critical path) on top, the title, then
 * the model, the estimate and what holds it: the dependencies it waits on,
 * or its PR and CI.
 */
export function TaskCard({ task, taskStatusMap, onClick, showStatus = false, showCriticalPath = false }: TaskCardProps) {
  const blockingDeps =
    taskStatusMap && task.status !== "done"
      ? task.dependencies.filter((id) => {
          const s = taskStatusMap[id]
          return s !== undefined && s !== "done"
        })
      : []
  const ci = task.ci_status ? CI[task.ci_status] : undefined
  const model = task.model.includes("/") ? task.model.split("/").pop() : task.model
  const critical = showCriticalPath && task.on_critical_path

  return (
    <div
      role={onClick ? "button" : undefined}
      tabIndex={onClick ? 0 : undefined}
      onClick={onClick ? () => onClick(task.id) : undefined}
      onKeyDown={
        onClick
          ? (e) => {
              if (e.key === "Enter" || e.key === " ") {
                e.preventDefault()
                onClick(task.id)
              }
            }
          : undefined
      }
      className={cn(
        "flex flex-col gap-2 rounded-lg border bg-card p-3 text-[13px] text-card-foreground transition-colors",
        onClick
          ? "cursor-pointer hover:border-input hover:bg-accent/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          : "cursor-default",
      )}
    >
      <div className="flex items-center gap-1.5">
        <span className="font-mono text-xs tabular-nums text-muted-foreground">{task.id}</span>
        {critical ? (
          <span className="ml-auto text-[11px] text-foreground">{t("task.on_critical_path")}</span>
        ) : task.parallelizable && task.status !== "done" ? (
          <span className="ml-auto text-[11px] text-status-done">{t("task.ready")}</span>
        ) : null}
      </div>

      <p className="line-clamp-3 font-medium leading-[18px]">{task.title}</p>

      <div className="flex flex-wrap items-center gap-1.5 text-xs text-muted-foreground">
        {showStatus ? <StatusBadge status={task.status} /> : null}
        {model ? (
          <span className="max-w-[140px] truncate rounded-full border px-2 py-px text-foreground/80" title={task.model}>
            {model}
          </span>
        ) : null}
        {task.estimate_hours != null ? <span className="tabular-nums">{t("task.hours", { hours: task.estimate_hours })}</span> : null}
        {blockingDeps.length > 0 ? (
          <span className="whitespace-nowrap text-status-blocked">{t("task.waiting_on", { count: blockingDeps.length })}</span>
        ) : null}
        {task.pr_url ? (
          <a
            href={task.pr_url}
            target="_blank"
            rel="noopener noreferrer"
            onClick={(e) => e.stopPropagation()}
            className={cn("whitespace-nowrap hover:underline", ci?.className)}
          >
            {t("task.pr")}
            {ci ? ` · ${t(ci.label)}` : ""}
          </a>
        ) : ci ? (
          <span className={cn("whitespace-nowrap", ci.className)}>{t(ci.label)}</span>
        ) : null}
      </div>
    </div>
  )
}
