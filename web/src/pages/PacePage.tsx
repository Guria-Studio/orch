import type { ReactNode } from "react"
import { Link } from "react-router-dom"
import { describeLoadError } from "@/lib/errors"
import { AlertTriangle } from "lucide-react"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Skeleton } from "@/components/ui/skeleton"
import { Explain } from "@/components/Explain"
import { PhaseSelect, usePhaseParam } from "@/components/PhaseSelect"
import { useMilestones } from "@/hooks/useMilestones"
import { useSprintHealth } from "@/hooks/useSprintHealth"
import { fmt, t } from "@/i18n"
import { taskPath } from "@/lib/nav"
import type { SprintBlocker } from "@/lib/types"

// Pace is the project's measured speed and the finish date it projects. The
// page used to be called "Sprint", but orch has no sprints: this is the whole
// project over a rolling 7-day window, and saying so is the point of the page.

function Stat({ value, label }: { value: string | number; label: ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col gap-1 rounded-lg border bg-card px-3.5 py-3">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="text-xl font-semibold tabular-nums tracking-[-0.01em]">{value}</dd>
    </div>
  )
}

function BlockerRow({ blocker }: { blocker: SprintBlocker }) {
  return (
    <li>
      <Link
        to={taskPath(blocker.task_id)}
        className="grid grid-cols-[0.5rem_auto_minmax(0,1fr)] items-center gap-x-3 gap-y-0.5 px-3.5 py-2.5 hover:bg-accent/50 sm:grid-cols-[0.5rem_auto_minmax(0,1fr)_auto]"
      >
        <span className="h-2 w-2 rounded-full bg-status-blocked" aria-hidden />
        <span className="font-mono text-xs text-muted-foreground">{blocker.task_id}</span>
        <span className="min-w-0">
          <span className="block truncate font-medium">{blocker.title}</span>
          {blocker.reason ? <span className="block truncate text-xs text-muted-foreground">{blocker.reason}</span> : null}
        </span>
        {blocker.blocked_at ? (
          <span className="col-start-3 text-xs text-muted-foreground sm:col-start-auto">
            {t("pace.blocked_since", { date: fmt.day(blocker.blocked_at) })}
          </span>
        ) : null}
      </Link>
    </li>
  )
}

export function PacePage() {
  const [phase] = usePhaseParam()
  const { data, isLoading, isError, error } = useSprintHealth(phase)
  const { data: milestones } = useMilestones()

  if (isLoading) {
    return (
      <div className="space-y-5">
        <Skeleton className="h-52 w-full rounded-lg" />
        <Skeleton className="h-32 w-full rounded-lg" />
      </div>
    )
  }

  if (isError || !data) {
    return (
      <Alert variant="destructive">
        <AlertTriangle className="h-4 w-4" />
        <AlertTitle>{t("pace.load_failed")}</AlertTitle>
        <AlertDescription>{describeLoadError(error)}</AlertDescription>
      </Alert>
    )
  }

  const velocity = data.velocity_per_day > 0 ? data.velocity_per_day.toFixed(1) : "—"

  return (
    <div className="space-y-5 text-[13px]">
      <div className="flex flex-wrap items-center gap-2">
        <p className="mr-auto text-muted-foreground">{t("pace.intro")}</p>
        <PhaseSelect phases={milestones ?? []} />
      </div>

      <section className="space-y-1.5 rounded-lg border bg-card p-4" aria-labelledby="finish-heading">
        <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
          <h2 id="finish-heading" className="text-xs font-medium text-muted-foreground">
            <Explain term="eta">{t("pace.projected_finish")}</Explain>
          </h2>
          {data.eta_date ? (
            <span className="text-2xl font-semibold tracking-[-0.02em] tabular-nums">{fmt.day(data.eta_date)}</span>
          ) : null}
        </div>
        {data.eta_date ? (
          <p className="flex flex-wrap items-center gap-x-3 gap-y-1 text-muted-foreground">
            {data.eta_days != null ? <span>{t("pace.in_days", { count: data.eta_days })}</span> : null}
            {data.confidence !== "none" ? (
              <Badge variant={data.confidence === "high" ? "success" : "warning"}>
                {t(data.confidence === "high" ? "pace.confidence.high" : "pace.confidence.low")}
              </Badge>
            ) : null}
            <span>{t("pace.excludes_blocked", { count: data.blocked_count })}</span>
          </p>
        ) : (
          <p className="text-muted-foreground">{t("pace.no_date")}</p>
        )}
      </section>

      <dl className="grid grid-cols-2 gap-3 sm:grid-cols-4" aria-label={t("pace.title")}>
        <Stat value={data.done_count} label={t("pace.stat.done")} />
        <Stat value={data.remaining_tasks} label={t("pace.stat.remaining")} />
        <Stat value={data.blocked_count} label={t("pace.stat.blocked")} />
        <Stat value={t("pace.per_day", { value: velocity })} label={<Explain term="velocity">{t("pace.stat.velocity")}</Explain>} />
      </dl>

      <details className="rounded-lg border bg-card px-3.5 py-2.5 text-xs text-muted-foreground">
        <summary className="cursor-pointer select-none font-medium text-foreground">{t("pace.how")}</summary>
        <div className="mt-2 space-y-1">
          <p>{t("pace.how_pace", { value: velocity })}</p>
          <p>{t("pace.how_finish", { count: data.remaining_tasks })}</p>
          <p>{t("pace.how_hours", { hours: data.remaining_hours })}</p>
        </div>
      </details>

      <section className="space-y-2" aria-labelledby="blocked-heading">
        <h2 id="blocked-heading" className="flex items-baseline gap-2 text-xs font-medium text-muted-foreground">
          {t("pace.blocked")}
          {data.blocked_count > 0 ? (
            <span className="font-normal text-status-blocked tabular-nums">{t("pace.blocked_count", { count: data.blocked_count })}</span>
          ) : null}
        </h2>
        {data.blockers.length === 0 ? (
          <p className="rounded-lg border border-dashed p-5 text-center text-muted-foreground">{t("pace.none_blocked")}</p>
        ) : (
          <ul className="divide-y overflow-hidden rounded-lg border bg-card">
            {data.blockers.map((b) => (
              <BlockerRow key={b.task_id} blocker={b} />
            ))}
          </ul>
        )}
      </section>
    </div>
  )
}
