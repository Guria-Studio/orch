import { useMemo, useRef } from "react"
import { useNavigate } from "react-router-dom"
import { describeLoadError } from "@/lib/errors"
import { AlertTriangle, Maximize2, Minimize2 } from "lucide-react"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { KanbanColumn, type KanbanStatus } from "@/components/KanbanColumn"
import { LiveStatusPill } from "@/components/LiveStatusPill"
import {
  TaskFiltersBar,
  toTaskFilters,
  useDebouncedValue,
  useTaskFilterParams,
} from "@/components/TaskFiltersBar"
import { taskPath } from "@/lib/nav"
import { EmptyTasks } from "@/components/EmptyTasks"
import { t } from "@/i18n"
import { criticalPathMatters, STATUS } from "@/lib/status"
import { useEventStream } from "@/hooks/useEventStream"
import { NewTaskButton } from "@/components/NewTaskButton"
import { useFullscreen } from "@/hooks/useFullscreen"
import { useTasks } from "@/hooks/useTasks"
import { cn } from "@/lib/utils"
import type { Task } from "@/lib/types"

const STATUS_TO_COLUMN: Record<string, KanbanStatus> = {
  backlog: "backlog",
  todo: "todo",
  "in-progress": "in_progress",
  blocked: "blocked",
  done: "done",
}

const COLUMNS: KanbanStatus[] = ["backlog", "todo", "in_progress", "blocked", "done"]

function groupTasks(tasks: Task[]): Record<KanbanStatus, Task[]> {
  const buckets: Record<KanbanStatus, Task[]> = {
    backlog: [],
    todo: [],
    in_progress: [],
    blocked: [],
    done: [],
  }
  for (const t of tasks) {
    const col = STATUS_TO_COLUMN[t.status] ?? "backlog"
    buckets[col].push(t)
  }
  return buckets
}

export function KanbanPage() {
  const [filters, setFilters] = useTaskFilterParams()
  const debouncedSearch = useDebouncedValue(filters.search, 250)

  const navigate = useNavigate()
  const openTask = (id: string) => navigate(taskPath(id))

  // Sprint E-6: fullscreen the board region (not the page shell) so operators
  // can zoom the columns without losing the sidebar/nav on exit.
  const boardRef = useRef<HTMLDivElement>(null)
  const [isFullscreen, toggleFullscreen] = useFullscreen(boardRef)

  const backendFilters = useMemo(
    () =>
      toTaskFilters({
        phase: filters.phase,
        model: filters.model,
        search: debouncedSearch,
        status: filters.status,
      }),
    [filters.phase, filters.model, debouncedSearch, filters.status],
  )

  const { data, isLoading, isError, error } = useTasks(backendFilters)

  // Derive model options from the (unfiltered) task list — but we only have
  // the filtered list here. Good enough: model options come from whatever is
  // currently visible. If the user clears filters, more models will appear.
  const modelOptions = useMemo(() => {
    if (!data) return [] as string[]
    const s = new Set<string>()
    for (const t of data.tasks) if (t.model) s.add(t.model)
    return Array.from(s).sort()
  }, [data])

  const phaseOptions = useMemo(() => {
    if (!data) return [] as number[]
    const s = new Set<number>()
    for (const t of data.tasks) s.add(t.phase)
    return Array.from(s).sort((a, b) => a - b)
  }, [data])

  // Kick off the SSE stream (invalidates the `tasks` query cache on events).
  // The pill in the header consumes this same result via props so we only
  // open one connection per page mount.
  const { status: streamStatus, lastEventAt } = useEventStream()

  const grouped = data ? groupTasks(data.tasks) : null

  const taskStatusMap = useMemo(() => {
    if (!data) return {} as Record<string, string>
    return Object.fromEntries(data.tasks.map((t) => [t.id, t.status]))
  }, [data])

  return (
    // On xl the board fills the viewport under the layout's header and each
    // column scrolls on its own; below that the page scrolls. The five
    // columns keep a 240px minimum and scroll sideways on a narrow screen.
    <div className="flex flex-col gap-3 text-[13px] xl:h-[calc(100vh-9rem)]">
      <h1 className="sr-only">{t("kanban.title")}</h1>
      {/* As in the design: the page's actions on top, then the filter bar on its own row. */}
      <div className="flex flex-shrink-0 flex-wrap items-center justify-end gap-2">
        <span className="text-xs tabular-nums text-muted-foreground">
          {data ? t("kanban.done_of", { done: data.summary.done, total: data.summary.total }) : t("tasks.loading")}
        </span>
        <Button type="button" variant="outline" size="sm" onClick={toggleFullscreen} className="gap-1.5">
          {isFullscreen ? <Minimize2 className="h-3.5 w-3.5" aria-hidden /> : <Maximize2 className="h-3.5 w-3.5" aria-hidden />}
          {isFullscreen ? t("common.exit_fullscreen") : t("common.fullscreen")}
        </Button>
        <NewTaskButton />
      </div>
      <div className="flex flex-shrink-0 flex-wrap items-center gap-2 border-b pb-3">
        <TaskFiltersBar value={filters} onChange={setFilters} phaseOptions={phaseOptions} modelOptions={modelOptions} />
        <span className="ml-auto">
          <LiveStatusPill status={streamStatus} lastEventAt={lastEventAt} />
        </span>
      </div>

      {isLoading ? (
        <div className="grid min-h-0 flex-1 grid-cols-[repeat(5,minmax(240px,1fr))] gap-3 overflow-x-auto">
          {Array.from({ length: 5 }).map((_, i) => (
            <Skeleton key={i} className="h-64 xl:h-full" />
          ))}
        </div>
      ) : isError ? (
        <Alert variant="destructive" className="flex-shrink-0">
          <AlertTriangle className="h-4 w-4" />
          <AlertTitle>{t("tasks.load_failed")}</AlertTitle>
          <AlertDescription>{describeLoadError(error)}</AlertDescription>
        </Alert>
      ) : !data || data.tasks.length === 0 ? (
        <EmptyTasks />
      ) : (
        <div ref={boardRef} className={cn("min-h-0 flex-1 overflow-x-auto bg-background", isFullscreen && "p-4")}>
          <div className="grid h-full min-w-[1240px] grid-cols-[repeat(5,minmax(240px,1fr))] gap-3 xl:grid-rows-1">
            {COLUMNS.map((status) => (
              <KanbanColumn
                key={status}
                title={t(STATUS[status].label)}
                status={status}
                showCriticalPath={criticalPathMatters(data.tasks)}
                tasks={grouped ? grouped[status] : []}
                taskStatusMap={taskStatusMap}
                onCardClick={openTask}
              />
            ))}
          </div>
        </div>
      )}
    </div>
  )
}
