import { TaskCard } from "@/components/TaskCard"
import { t } from "@/i18n"
import { STATUS } from "@/lib/status"
import { cn } from "@/lib/utils"
import type { Task } from "@/lib/types"

export type KanbanStatus = "backlog" | "todo" | "in_progress" | "blocked" | "done"

export interface KanbanColumnProps {
  title: string
  status: KanbanStatus
  tasks: Task[]
  taskStatusMap?: Record<string, string>
  onCardClick?: (taskId: string) => void
  showCriticalPath?: boolean
}

/**
 * An open lane, Linear-style: no fill, no border — the status lives in the
 * header (a ring in its color, the name, the count), so the cards carry the
 * attention.
 */
export function KanbanColumn({ title, status, tasks, taskStatusMap, onCardClick, showCriticalPath }: KanbanColumnProps) {
  return (
    <section className="flex h-full min-h-[200px] min-w-0 flex-col gap-2" aria-label={title}>
      <header className="flex h-7 flex-shrink-0 items-center gap-2 px-1 text-[13px] font-medium">
        <span className={cn("h-3 w-3 rounded-full border-2 border-current", STATUS[status].text)} aria-hidden />
        {title}
        <span className="font-normal tabular-nums text-muted-foreground">{tasks.length}</span>
      </header>
      <div className="min-h-0 flex-1 space-y-2 overflow-y-auto">
        {tasks.length === 0 ? (
          <p className="rounded-lg border border-dashed p-4 text-center text-[13px] text-muted-foreground">{t("kanban.empty_column")}</p>
        ) : (
          tasks.map((task) => (
            <TaskCard key={task.id} task={task} taskStatusMap={taskStatusMap} onClick={onCardClick} showCriticalPath={showCriticalPath} />
          ))
        )}
      </div>
    </section>
  )
}
