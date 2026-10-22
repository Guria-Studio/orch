import { useRef, useState, type CSSProperties, type ReactNode } from "react"
import { AlertTriangle, ArrowLeft } from "lucide-react"
import { Link, useLocation, useNavigate, useParams } from "react-router-dom"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { ActivityFeed } from "@/components/ActivityFeed"
import { Explain } from "@/components/Explain"
import { StatusBadge } from "@/components/StatusBadge"
import { TaskSteer } from "@/components/TaskSteer"
import { useTicker } from "@/hooks/useNow"
import { useTaskDetail } from "@/hooks/useTaskDetail"
import { fmt, t } from "@/i18n"
import { taskPath } from "@/lib/nav"
import { statusKey } from "@/lib/status"
import { formatRelative } from "@/lib/time"
import { cn } from "@/lib/utils"
import type { Task } from "@/lib/types"

function Meta({ label, children }: { label: ReactNode; children: ReactNode }) {
  return (
    <div className="min-w-0">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="mt-0.5 truncate text-sm tabular-nums">{children}</dd>
    </div>
  )
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="border-t pt-4">
      <h3 className="mb-2 text-xs font-medium text-muted-foreground">{title}</h3>
      {children}
    </section>
  )
}

interface TaskComment {
  at: string
  author: string
  body: string
  kind?: string
}

/** A comment as the API sends it, or null for an entry that is not one. */
function asComment(raw: unknown): TaskComment | null {
  if (!raw || typeof raw !== "object") return null
  const c = raw as Record<string, unknown>
  if (typeof c.body !== "string" || c.body.trim() === "") return null
  return {
    at: typeof c.at === "string" ? c.at : "",
    author: typeof c.author === "string" && c.author ? c.author : "—",
    body: c.body,
    kind: typeof c.kind === "string" ? c.kind : undefined,
  }
}

// The task's comment trail, oldest first (#405): who wrote it, when, and what.
// orch's own notes mostly repeat a status the badge above already shows, so
// they are muted next to what an agent or the operator wrote.
function TaskComments({ comments, label }: { comments: unknown[]; label: string }) {
  const now = useTicker(60_000)
  const rows = comments.map(asComment).filter((c): c is TaskComment => c !== null)
  return (
    <ul aria-label={label} className="max-h-80 space-y-2 overflow-auto pr-1">
      {rows.map((c, i) => {
        const muted = c.author === "orch"
        return (
          <li key={`${c.at}-${i}`} data-muted={muted ? "true" : undefined} className={cn("text-xs", muted && "text-muted-foreground")}>
            <div className="flex flex-wrap items-baseline gap-x-1.5">
              <span className={cn("font-medium", !muted && "text-foreground")}>{c.author}</span>
              {c.kind === "operator_note" ? <Badge variant="outline" className="px-1 py-0 text-[10px] font-normal">{t("task.operator_note")}</Badge> : null}
              {c.at ? (
                <time dateTime={c.at} title={fmt.dateTime(c.at)} className="text-muted-foreground">
                  {formatRelative(c.at, now)}
                </time>
              ) : null}
            </div>
            <p className="whitespace-pre-wrap break-words leading-relaxed">{c.body}</p>
          </li>
        )
      })}
    </ul>
  )
}

// What the task is: metadata, its context and the operator's controls. It
// sits beside the activity, which is what a task's page is opened for.
function TaskContext({ task }: { task: Task }) {
  return (
    <div className="space-y-4">
      <dl className="grid grid-cols-2 gap-x-4 gap-y-3">
        <Meta label={t("tasks.col.status")}>
          <StatusBadge status={task.status} />
        </Meta>
        <Meta label={t("tasks.col.phase")}>{task.phase}</Meta>
        <Meta label={<Explain term="estimate" />}>{t("task.hours", { hours: task.estimate_hours })}</Meta>
        <Meta label={<Explain term="agent_time" />}>{t("task.hours", { hours: task.human_hours })}</Meta>
        <Meta label={t("tasks.col.model")}>
          <span className="font-mono text-xs" title={task.model}>
            {task.model || "—"}
          </span>
        </Meta>
        <Meta label={<Explain term="downstream_impact" />}>{task.downstream_impact}</Meta>
        <Meta label={<Explain term="ready" />}>{task.parallelizable ? t("common.yes") : t("common.no")}</Meta>
        <Meta label={t("tasks.col.updated")}>{fmt.dateTime(task.last_updated)}</Meta>
      </dl>

      {task.description ? (
        <Section title={t("task.description")}>
          <p className="whitespace-pre-wrap text-sm leading-relaxed">{task.description}</p>
        </Section>
      ) : null}

      {task.reason ? (
        <Section title={t("task.reason")}>
          <p className="whitespace-pre-wrap text-sm leading-relaxed">{task.reason}</p>
        </Section>
      ) : null}

      {task.dependencies.length > 0 ? (
        <Section title={t("task.dependencies", { count: task.dependencies.length })}>
          <ul className="flex flex-wrap gap-1.5">
            {task.dependencies.map((d) => (
              <li key={d}>
                <Badge variant="outline" className="font-mono text-xs font-normal">
                  {d}
                </Badge>
              </li>
            ))}
          </ul>
        </Section>
      ) : null}

      {task.files.length > 0 ? (
        <Section title={t("task.files", { count: task.files.length })}>
          <ul className="space-y-1 text-xs">
            {task.files.map((f) => (
              <li key={f} className="truncate rounded-md bg-muted px-2 py-1 font-mono">
                {f}
              </li>
            ))}
          </ul>
        </Section>
      ) : null}

      {task.spec_ref ? (
        <Section title={t("task.spec_ref")}>
          <span className="font-mono text-xs">{task.spec_ref}</span>
        </Section>
      ) : null}

      {task.comments.length > 0 ? (
        <Section title={t("task.comments", { count: task.comments.length })}>
          <TaskComments comments={task.comments} label={t("task.comments", { count: task.comments.length })} />
        </Section>
      ) : null}

      <TaskSteer task={task} />

    </div>
  )
}

const SPLIT_KEY = "orch.taskSplit"
const SPLIT_DEFAULT = 70
const SPLIT_MIN = 20
const SPLIT_STEP = 5

const clampSplit = (n: number) => Math.min(100 - SPLIT_MIN, Math.max(SPLIT_MIN, Math.round(n)))

function storedSplit(): number {
  try {
    const n = Number(window.localStorage.getItem(SPLIT_KEY))
    return n ? clampSplit(n) : SPLIT_DEFAULT
  } catch {
    return SPLIT_DEFAULT
  }
}

/**
 * The details beside the activity, 70/30 by default (#404), with a handle
 * between them: drag it, or focus it and use the arrow keys; a double click
 * puts it back. Each side keeps 20%. The split is this browser's, kept in
 * localStorage. Below lg the two stack, as they always did, with no handle.
 */
function SplitGrid({ children }: { children: [ReactNode, ReactNode] }) {
  const [split, setSplitState] = useState(storedSplit)
  const grid = useRef<HTMLDivElement>(null)
  const setSplit = (n: number) => {
    const next = clampSplit(n)
    setSplitState(next)
    try {
      window.localStorage.setItem(SPLIT_KEY, String(next))
    } catch {
      // A browser that keeps nothing still resizes; it just forgets.
    }
  }
  const drag = (e: React.PointerEvent<HTMLDivElement>) => {
    const box = grid.current?.getBoundingClientRect()
    if (!box || box.width === 0) return
    e.currentTarget.setPointerCapture(e.pointerId)
    const move = (ev: PointerEvent) => setSplit(((ev.clientX - box.left) / box.width) * 100)
    const up = () => {
      window.removeEventListener("pointermove", move)
      window.removeEventListener("pointerup", up)
    }
    window.addEventListener("pointermove", move)
    window.addEventListener("pointerup", up)
  }
  const keys: Record<string, number> = { ArrowLeft: -SPLIT_STEP, ArrowRight: SPLIT_STEP }
  const [details, activity] = children
  return (
    <div
      ref={grid}
      className="grid gap-6 lg:grid-cols-[minmax(0,var(--details))_auto_minmax(0,var(--activity))] lg:gap-0"
      style={{ "--details": `${split}fr`, "--activity": `${100 - split}fr` } as CSSProperties}
    >
      {details}
      <div
        role="separator"
        aria-orientation="vertical"
        aria-label={t("task.resize")}
        aria-valuemin={SPLIT_MIN}
        aria-valuemax={100 - SPLIT_MIN}
        aria-valuenow={split}
        tabIndex={0}
        onPointerDown={drag}
        onDoubleClick={() => setSplit(SPLIT_DEFAULT)}
        onKeyDown={(e) => {
          if (keys[e.key]) {
            e.preventDefault()
            setSplit(split + keys[e.key])
          } else if (e.key === "Home" || e.key === "Enter") {
            e.preventDefault()
            setSplit(SPLIT_DEFAULT)
          }
        }}
        className="group hidden cursor-col-resize touch-none px-2 focus-visible:outline-none lg:flex lg:justify-center"
      >
        <span className="h-full w-px bg-border transition-colors group-hover:bg-primary/60 group-focus-visible:w-0.5 group-focus-visible:bg-primary" />
      </div>
      {activity}
    </div>
  )
}

/**
 * A task's own page (#374): /work/tasks/<id>. The activity feed is the main
 * area, with its own scroll, followed live while the task runs; the context
 * column stays beside it. Having an address, it opens in a new tab, survives a
 * reload and can be shared.
 */
export function TaskPage() {
  const { id = "" } = useParams()
  const { data, isLoading, isError, error } = useTaskDetail(id)
  const navigate = useNavigate()
  const location = useLocation()
  // Back to wherever the operator came from, filters and all; a page opened
  // straight from a link has nowhere to go back to, so it goes to the list.
  const back = () => (location.key === "default" ? navigate("/work/list") : navigate(-1))

  return (
    <div className="space-y-4">
      <Button variant="ghost" size="sm" onClick={back} className="-ml-2 gap-1 text-muted-foreground">
        <ArrowLeft className="h-4 w-4" aria-hidden />
        {t("task.back")}
      </Button>
      {isLoading ? (
        <div className="space-y-4">
          <Skeleton className="h-6 w-1/2" />
          <Skeleton className="h-4 w-2/3" />
          <Skeleton className="h-64 w-full" />
        </div>
      ) : isError ? (
        <Alert variant="destructive">
          <AlertTriangle className="h-4 w-4" />
          <AlertTitle>{t("task.load_failed")}</AlertTitle>
          <AlertDescription>{error?.message ?? t("common.unknown_error")}</AlertDescription>
        </Alert>
      ) : data ? (
        <>
          <header className="min-w-0">
            <p className="font-mono text-xs text-muted-foreground">{data.id}</p>
            <h1 className="text-xl font-semibold leading-snug">{data.title}</h1>
          </header>
          <SplitGrid>
            <aside className="min-w-0 lg:sticky lg:top-4 lg:max-h-[calc(100dvh-8rem)] lg:self-start lg:overflow-auto">
              <TaskContext task={data} />
            </aside>
            {/* The last dispatch's steps. The model's route names its provider: "claude/sonnet". */}
            <section className="min-w-0 max-lg:order-first" aria-labelledby="task-activity">
              <h2 id="task-activity" className="mb-2 text-xs font-medium text-muted-foreground">
                {t("activity.title")}
              </h2>
              <ActivityFeed
                key={data.id}
                taskId={data.id}
                live={statusKey(data.status) === "in_progress"}
                provider={data.model.split("/")[0]}
                className="rounded-md border bg-muted/30 p-3"
                full
              />
            </section>
          </SplitGrid>
        </>
      ) : null}
    </div>
  )
}

/** A link to a task's page, for places that list tasks. */
export function TaskLink({ id, className, children }: { id: string; className?: string; children: ReactNode }) {
  return (
    <Link to={taskPath(id)} className={className}>
      {children}
    </Link>
  )
}
