import { useMemo } from "react"
import { Link } from "react-router-dom"
import { AlertTriangle, ArrowDownUp, GitBranch, Lock, Route } from "lucide-react"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Skeleton } from "@/components/ui/skeleton"
import { EmptyTasks } from "@/components/EmptyTasks"
import { StatusRing } from "@/components/StatusRing"
import { Explain } from "@/components/Explain"
import { LiveStatusPill } from "@/components/LiveStatusPill"
import { NewTaskButton } from "@/components/NewTaskButton"
import { taskPath } from "@/lib/nav"
import {
  TaskFiltersBar,
  toTaskFilters,
  useDebouncedValue,
  useTaskFilterParams,
} from "@/components/TaskFiltersBar"
import { useEventStream } from "@/hooks/useEventStream"
import { useTableSort, type SortGetters } from "@/hooks/useTableSort"
import { useTasks } from "@/hooks/useTasks"
import { fmt, t } from "@/i18n"
import { describeLoadError } from "@/lib/errors"
import { criticalPathMatters, STATUS, statusKey, type StatusKey } from "@/lib/status"
import { cn } from "@/lib/utils"
import type { Task } from "@/lib/types"

const SORT_GETTERS: SortGetters<Task> = {
  phase: (t) => t.phase,
  id: (t) => t.id,
  title: (t) => t.title,
  model: (t) => t.model,
  dep_count: (t) => t.dep_count,
  estimate_hours: (t) => t.estimate_hours,
  last_updated: (t) => t.last_updated,
}

const SORT_OPTIONS = [
  ["phase", "tasks.col.phase"],
  ["id", "tasks.col.id"],
  ["title", "tasks.col.title"],
  ["model", "tasks.col.model"],
  ["dep_count", "tasks.col.deps"],
  ["estimate_hours", "tasks.col.estimate"],
  ["last_updated", "tasks.col.updated"],
] as const

// Group order: what is moving first, what is finished last.
const GROUP_ORDER: StatusKey[] = ["in_progress", "todo", "blocked", "failed", "backlog", "done"]

/**
 * Work › List, as a Linear issue list: tasks grouped by status under
 * collapsible headers, one 40px row per task — status, id, title, model,
 * phase, estimate — each row a link to the task's page. Filters live in the
 * query string; the sort applies within each group.
 */
export function ListPage() {
  const [filters, setFilters] = useTaskFilterParams()
  const debouncedSearch = useDebouncedValue(filters.search, 250)

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
  const { status: streamStatus, lastEventAt } = useEventStream()

  const { sort, toggleSort, sortedRows } = useTableSort<Task>({ column: "phase", direction: "asc" })

  const groups = useMemo(() => {
    if (!data) return [] as { key: StatusKey; tasks: Task[] }[]
    const byId = [...data.tasks].sort((a, b) => (a.id < b.id ? -1 : a.id > b.id ? 1 : 0))
    const sorted = sortedRows(byId, SORT_GETTERS)
    return GROUP_ORDER.map((key) => ({ key, tasks: sorted.filter((task) => statusKey(task.status) === key) })).filter(
      (g) => g.tasks.length > 0,
    )
  }, [data, sortedRows])

  // Used to mark a task whose dependencies are not done yet.
  const taskStatusMap = useMemo(() => {
    if (!data) return {} as Record<string, string>
    return Object.fromEntries(data.tasks.map((task) => [task.id, task.status]))
  }, [data])

  const modelOptions = useMemo(() => {
    if (!data) return [] as string[]
    const s = new Set<string>()
    for (const task of data.tasks) if (task.model) s.add(task.model)
    return Array.from(s).sort()
  }, [data])

  const phaseOptions = useMemo(() => {
    if (!data) return [] as number[]
    const s = new Set<number>()
    for (const task of data.tasks) s.add(task.phase)
    return Array.from(s).sort((a, b) => a - b)
  }, [data])

  const showCriticalPath = data ? criticalPathMatters(data.tasks) : false
  const isEmpty = !isLoading && (!data || data.tasks.length === 0)

  return (
    <div className="space-y-4 text-[13px]">
      {/* The layout's header names the page, as in the design; the title stays for screen readers. */}
      <h1 className="sr-only">{t("tasks.title")}</h1>
      <header className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <p className="text-xs text-muted-foreground tabular-nums">
            {data ? t("tasks.shown", { count: data.count, total: data.total }) : t("tasks.loading")}
            {showCriticalPath ? (
              <span className="ml-3 inline-flex items-center gap-1 align-middle">
                <Route className="h-3.5 w-3.5 text-foreground" aria-hidden />
                <Explain term="critical_path" />
              </span>
            ) : null}
          </p>
        </div>
        <div className="flex items-center gap-2">
          <LiveStatusPill status={streamStatus} lastEventAt={lastEventAt} />
          <NewTaskButton />
          <label className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <span className="sr-only">{t("tasks.sort")}</span>
            <select
              value={sort.column}
              onChange={(e) => toggleSort(e.target.value)}
              title={t("tasks.sort")}
              className="h-7 rounded-md border bg-card px-2 text-xs text-foreground max-md:h-10"
            >
              {SORT_OPTIONS.map(([key, label]) => (
                <option key={key} value={key}>
                  {t(label)}
                </option>
              ))}
            </select>
          </label>
          <button
            type="button"
            onClick={() => toggleSort(sort.column)}
            aria-label={t("tasks.sort_reverse")}
            aria-pressed={sort.direction === "desc"}
            title={t("tasks.sort_reverse")}
            className={cn(
              "flex h-7 w-7 items-center justify-center rounded-md border bg-card text-muted-foreground hover:text-foreground max-md:h-10 max-md:w-10",
              "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
              sort.direction === "desc" && "text-foreground",
            )}
          >
            <ArrowDownUp className="h-3.5 w-3.5" aria-hidden />
          </button>
        </div>
      </header>

      <TaskFiltersBar value={filters} onChange={setFilters} phaseOptions={phaseOptions} modelOptions={modelOptions} />

      {isError ? (
        <Alert variant="destructive">
          <AlertTriangle className="h-4 w-4" />
          <AlertTitle>{t("tasks.load_failed")}</AlertTitle>
          <AlertDescription>{describeLoadError(error)}</AlertDescription>
        </Alert>
      ) : isEmpty ? (
        <EmptyTasks filtered={Object.values(backendFilters).some(Boolean)} />
      ) : isLoading ? (
        <div className="space-y-px overflow-hidden rounded-lg border">
          {Array.from({ length: 8 }).map((_, i) => (
            <Skeleton key={i} className="h-10 w-full rounded-none" />
          ))}
        </div>
      ) : (
        <div className="overflow-hidden rounded-lg border">
          {groups.map(({ key, tasks }) => {
            const meta = STATUS[key]
            return (
              <details key={key} open className="group/status border-b last:border-b-0">
                <summary className="flex h-9 cursor-pointer list-none items-center gap-2 border-b bg-card px-5 font-medium select-none [&::-webkit-details-marker]:hidden">
                  <StatusRing status={key} />
                  {t(meta.label)}
                  <span className="font-normal tabular-nums text-muted-foreground">{tasks.length}</span>
                </summary>
                <ul aria-label={t(meta.label)} className="divide-y divide-border/60">
                  {tasks.map((task) => {
                    const blocking = task.dependencies.some(
                      (id) => taskStatusMap[id] !== undefined && taskStatusMap[id] !== "done",
                    )
                    return (
                      <li key={task.id}>
                        <Link
                          to={taskPath(task.id)}
                          title={task.last_updated ? `${task.title} · ${fmt.dateTime(task.last_updated)}` : task.title}
                          className={cn(
                            "grid h-10 grid-cols-[16px_auto_minmax(0,1fr)_auto] items-center gap-x-3.5 px-5 transition-colors hover:bg-accent/50",
                            "md:grid-cols-[16px_64px_minmax(0,1fr)_96px_72px_56px]",
                            "focus-visible:bg-accent/50 focus-visible:outline-none",
                          )}
                        >
                          <StatusRing status={key} label={t(meta.label)} />
                          <span className="truncate font-mono text-xs text-muted-foreground">{task.id}</span>
                          <span className="flex min-w-0 items-center gap-1.5">
                            {showCriticalPath && task.on_critical_path ? (
                              <Route className="h-3.5 w-3.5 shrink-0" aria-label={t("task.on_critical_path")} />
                            ) : null}
                            <span className="truncate">{task.title}</span>
                            {blocking ? (
                              <Lock className="h-3 w-3 shrink-0 text-status-blocked" aria-label={t("task.deps_pending")} />
                            ) : null}
                            {task.parallelizable ? (
                              <GitBranch className="h-3.5 w-3.5 shrink-0 text-status-done" aria-label={t("task.ready")} />
                            ) : null}
                          </span>
                          <span className="hidden min-w-0 md:block">
                            {task.model ? (
                              <span
                                className="inline-block max-w-full truncate rounded-full border px-2 py-0.5 text-xs text-foreground/80"
                                title={task.model}
                              >
                                {task.model.split("/")[0]}
                              </span>
                            ) : null}
                          </span>
                          <span className="hidden text-xs text-muted-foreground md:block">F{task.phase}</span>
                          <span className="text-right text-xs tabular-nums text-muted-foreground">
                            {t("task.hours", { hours: task.estimate_hours })}
                          </span>
                        </Link>
                      </li>
                    )
                  })}
                </ul>
              </details>
            )
          })}
        </div>
      )}
    </div>
  )
}
