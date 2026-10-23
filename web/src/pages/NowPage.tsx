import { useState } from "react"
import { Link } from "react-router-dom"
import { AlertTriangle, Eye } from "lucide-react"
import { ActivityFeed } from "@/components/ActivityFeed"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { Skeleton } from "@/components/ui/skeleton"
import { CopyCommand } from "@/components/CopyCommand"
import { Explain } from "@/components/Explain"
import { FirstRunChecklist } from "@/components/FirstRunChecklist"
import { RunReceipt } from "@/components/RunReceipt"
import { useBudgetSummary, type BudgetRow } from "@/hooks/useBudgetSummary"
import { useOnboarding, useReceipt } from "@/hooks/useReceipt"
import { useMilestones } from "@/hooks/useMilestones"
import { useEstimate, useNow, useTicker, type Estimate, type NowDispatch } from "@/hooks/useNow"
import { useTasks } from "@/hooks/useTasks"
import { fmt, t } from "@/i18n"
import { describeLoadError } from "@/lib/errors"
import { taskPath } from "@/lib/nav"
import { formatDuration, formatElapsed, formatRelative } from "@/lib/time"
import { cn } from "@/lib/utils"
import type { SprintBlocker } from "@/lib/types"

// The operator's first screen: the run line, three figures (tasks done, the
// agents at work, the finish date), then what runs, what needs a person and
// what the next run would take; beside it the phases, the budget window and
// the last run's receipt.

const panel = "overflow-hidden rounded-lg border bg-card"
const rowGrid = "grid items-center gap-x-3 px-3.5 py-2.5"

/** A section title with an optional status dot and count, as the design draws it. */
function Heading({ id, title, dot, aside }: { id: string; title: string; dot?: string; aside?: React.ReactNode }) {
  return (
    <div className="mb-2 flex items-center gap-2">
      {dot ? <span className={cn("h-1.5 w-1.5 rounded-full", dot)} aria-hidden /> : null}
      <h2 id={id} className="text-xs font-medium text-muted-foreground">
        {title}
      </h2>
      {aside != null ? <span className="ml-auto text-xs tabular-nums text-muted-foreground">{aside}</span> : null}
    </div>
  )
}

function Kpi({ label, value, sub, children }: { label: React.ReactNode; value: React.ReactNode; sub?: React.ReactNode; children?: React.ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col gap-1 rounded-lg border bg-card px-3.5 py-3">
      <span className="text-xs text-muted-foreground">{label}</span>
      <span className="truncate text-xl font-semibold tabular-nums tracking-[-0.01em]">{value}</span>
      {sub ? <span className="truncate text-xs text-muted-foreground">{sub}</span> : null}
      {children}
    </div>
  )
}

// An agent at work: id and title open its page; the eye follows what it is
// doing, live, below the row.
function WorkingRow({ d, now }: { d: NowDispatch; now: number }) {
  const [open, setOpen] = useState(false)
  const started = new Date(d.started_at).getTime()
  const attempt =
    d.max_attempts > 0 ? t("now.attempt", { attempt: d.attempt, max: d.max_attempts }) : t("now.attempt_n", { attempt: d.attempt })
  const activityId = `activity-${d.task_id}`
  return (
    <li>
      <div className={cn(rowGrid, "grid-cols-[minmax(0,1fr)_auto] sm:grid-cols-[4.5rem_minmax(0,1fr)_auto_auto_auto_auto]")}>
        <Link to={taskPath(d.task_id)} className="hidden font-mono text-xs text-muted-foreground hover:text-foreground sm:block">
          {d.task_id}
        </Link>
        <Link to={taskPath(d.task_id)} className="min-w-0 truncate font-medium hover:underline">
          {d.title}
        </Link>
        <span className="hidden font-mono text-xs text-muted-foreground sm:block">{d.model || d.provider}</span>
        <span className="hidden text-xs text-muted-foreground sm:block">{attempt}</span>
        <span
          className="inline-flex items-center gap-2 font-mono text-xs tabular-nums text-status-running"
          title={Number.isNaN(started) ? undefined : fmt.dateTime(d.started_at)}
          aria-label={Number.isNaN(started) ? undefined : t("now.elapsed", { elapsed: formatElapsed(now - started) })}
        >
          <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-status-running motion-reduce:animate-none" aria-hidden />
          {Number.isNaN(started) ? "—" : formatElapsed(now - started)}
        </span>
        <button
          type="button"
          onClick={() => setOpen((o) => !o)}
          aria-expanded={open}
          aria-controls={activityId}
          aria-label={t("activity.show", { id: d.task_id })}
          className="hidden h-7 w-7 items-center justify-center rounded-md text-muted-foreground hover:bg-secondary hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring sm:inline-flex"
        >
          <Eye className="h-3.5 w-3.5" aria-hidden />
        </button>
      </div>
      <div id={activityId} hidden={!open} className="border-t bg-muted/30 px-3.5 py-3">
        {open ? <ActivityFeed taskId={d.task_id} live provider={d.provider} /> : null}
      </div>
    </li>
  )
}

function BlockedRow({ b, now }: { b: SprintBlocker; now: number }) {
  return (
    <li className={cn(rowGrid, "grid-cols-[minmax(0,1fr)_auto] sm:grid-cols-[4.5rem_minmax(0,1fr)_auto]")}>
      <Link to={taskPath(b.task_id)} className="hidden font-mono text-xs text-muted-foreground hover:text-foreground sm:block">
        {b.task_id}
      </Link>
      <span className="min-w-0">
        <span className="block truncate font-medium">{b.title}</span>
        <span className="block truncate text-xs text-muted-foreground" title={b.reason}>
          {b.blocked_at ? `${t("now.blocked_since", { time: formatRelative(b.blocked_at, now) })} · ` : null}
          {b.reason}
        </span>
      </span>
      <Popover>
        <PopoverTrigger asChild>
          <Button type="button" size="sm" variant="outline">
            {t("now.unblock")}
          </Button>
        </PopoverTrigger>
        <PopoverContent align="end" className="w-[22rem]">
          <p className="font-medium">{t("now.unblock_title", { id: b.task_id })}</p>
          <p className="mt-1 text-muted-foreground">{t("now.unblock_detail")}</p>
          <CopyCommand command={`orch task set --id ${b.task_id} --status todo`} />
        </PopoverContent>
      </Popover>
    </li>
  )
}

function Meter({ pct, tone, label }: { pct: number; tone: string; label: string }) {
  return (
    <div className="h-1.5 overflow-hidden rounded-full bg-muted" role="meter" aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.round(pct)} aria-label={label}>
      <div className={cn("h-full rounded-full transition-[width]", tone)} style={{ width: `${pct}%` }} />
    </div>
  )
}

function BudgetGauge({ row, now }: { row: BudgetRow; now: number }) {
  const pct = row.token_budget > 0 ? Math.min(100, (row.tokens_used / row.token_budget) * 100) : 0
  return (
    <li className="space-y-1.5 px-3.5 py-2.5">
      <div className="flex flex-wrap items-baseline gap-x-2">
        <span className="font-medium">{row.provider}</span>
        <span className="text-xs text-muted-foreground">{t("now.window", { hours: row.window_hours })}</span>
        <span className="ml-auto font-mono text-xs tabular-nums text-muted-foreground">
          {t("now.tokens", { used: fmt.number(row.tokens_used), budget: fmt.number(row.token_budget) })}
        </span>
      </div>
      <Meter pct={pct} tone={row.over_threshold ? "bg-status-blocked" : "bg-brand"} label={row.provider} />
      {row.reset_at ? <p className="text-[11px] text-muted-foreground">{t("now.resets", { time: formatRelative(row.reset_at, now) })}</p> : null}
    </li>
  )
}

// What the next `orch run` would take, shown while nothing runs (#357).
function NextRun({ e }: { e: Estimate }) {
  return (
    <section aria-labelledby="now-next">
      <Heading id="now-next" title={t("now.next_title")} />
      <div className={cn(panel, "px-3.5 py-3")}>
        <p className="tabular-nums">
          {t("now.next_run", { count: e.tasks.length, cost: fmt.usd(e.cost_usd), time: formatDuration(e.wall_s), slots: e.slots })}
        </p>
        <p className="mt-0.5 text-xs text-muted-foreground">
          {e.confidence === "none"
            ? t("now.next_run_rough")
            : t("now.next_run_basis", { count: e.past_attempts, confidence: t(`now.confidence.${e.confidence}`) })}
        </p>
      </div>
    </section>
  )
}

export function NowPage() {
  const now = useTicker()
  const { data, isLoading, isError, error } = useNow()
  const { data: phases } = useMilestones()
  const { data: budget } = useBudgetSummary()
  const { data: tasks } = useTasks()
  const { data: receipt } = useReceipt()
  const onboarding = useOnboarding()
  const idle = !!data && data.working.length === 0 && data.run?.status !== "live"
  const { data: estimate } = useEstimate(idle)

  if (isLoading) {
    return (
      <div className="space-y-6">
        <div className="grid gap-3 sm:grid-cols-3">
          <Skeleton className="h-20" />
          <Skeleton className="h-20" />
          <Skeleton className="h-20" />
        </div>
        <Skeleton className="h-48 w-full" />
      </div>
    )
  }

  if (isError || !data) {
    return (
      <Alert variant="destructive">
        <AlertTriangle className="h-4 w-4" />
        <AlertTitle>{t("now.load_failed")}</AlertTitle>
        <AlertDescription>{describeLoadError(error)}</AlertDescription>
      </Alert>
    )
  }

  const titles = new Map((tasks?.tasks ?? []).map((task) => [task.id, task.title]))
  const { summary, pace, run } = data
  const attentionCount = data.attention.length + data.waiting_for_budget.length
  const donePct = summary.total > 0 ? (summary.done / summary.total) * 100 : 0
  const slots =
    data.slots.max > 0
      ? t("now.slots", { used: data.slots.used, max: data.slots.max })
      : t("now.slots_unknown", { used: data.slots.used, count: data.slots.used })
  const r = receipt?.receipt

  return (
    <div className="space-y-6 text-[13px]">
      {run ? (
        <div className="flex justify-end">
          <span
            className={cn(
              "inline-flex items-center gap-2 rounded-full px-2.5 py-0.5 text-xs",
              run.status === "live" ? "bg-status-done/10 text-status-done" : "bg-secondary text-muted-foreground",
            )}
          >
            <span className={cn("h-1.5 w-1.5 rounded-full", run.status === "live" ? "bg-status-done" : "bg-muted-foreground")} aria-hidden />
            {run.status === "live"
              ? t("now.run_live", { id: run.run_id, time: formatRelative(run.started_at, now) })
              : t("now.run_done", { id: run.run_id, time: formatRelative(run.updated_at || run.started_at, now) })}
          </span>
        </div>
      ) : null}

      <section aria-label={t("now.title")} className="grid gap-3 sm:grid-cols-3">
        <Kpi label={t("now.kpi.done")} value={t("now.tasks_done", { done: summary.done, total: summary.total })}>
          <Meter pct={donePct} tone="bg-brand" label={t("now.kpi.done")} />
        </Kpi>
        <Kpi
          label={t("now.kpi.agents")}
          value={data.working.length > 0 ? t("now.agents_working", { count: data.working.length }) : t("now.no_agents")}
          sub={slots}
        />
        <Kpi
          label={<Explain term="eta">{t("now.kpi.eta")}</Explain>}
          value={pace.eta_date ? fmt.day(pace.eta_date) : "—"}
          sub={pace.eta_date ? (pace.blocked_count > 0 ? t("now.eta_excludes", { count: pace.blocked_count }) : undefined) : t("now.no_eta")}
        />
      </section>

      {onboarding.data && !onboarding.data.complete ? (
        <FirstRunChecklist onboarding={onboarding.data} onRecheck={() => void onboarding.refetch()} checking={onboarding.isFetching} />
      ) : !run ? (
        <p className="text-muted-foreground">
          {t("now.never_ran")} <code className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs">orch run</code>
        </p>
      ) : null}

      <div className="grid gap-6 xl:grid-cols-[minmax(0,1.6fr)_minmax(0,1fr)]">
        <div className="min-w-0 space-y-6">
          <section aria-labelledby="now-working">
            <Heading id="now-working" title={t("now.working")} dot="bg-status-running" aside={slots} />
            {data.working.length > 0 ? (
              <ul className={cn(panel, "divide-y")}>
                {data.working.map((d) => (
                  <WorkingRow key={`${d.task_id}-${d.started_at}`} d={d} now={now} />
                ))}
              </ul>
            ) : (
              <p className="rounded-lg border border-dashed px-3.5 py-5 text-muted-foreground">{t("now.nothing_running")}</p>
            )}
          </section>

          <section aria-labelledby="now-attention">
            <Heading id="now-attention" title={t("now.attention")} dot="bg-status-blocked" aside={attentionCount > 0 ? attentionCount : undefined} />
            {attentionCount > 0 ? (
              <ul className={cn(panel, "divide-y")}>
                {data.attention.map((b) => (
                  <BlockedRow key={b.task_id} b={b} now={now} />
                ))}
                {data.waiting_for_budget.map((w) => (
                  <li key={w.task_id} className={cn(rowGrid, "grid-cols-1 sm:grid-cols-[4.5rem_minmax(0,1fr)]")}>
                    <Link to={taskPath(w.task_id)} className="hidden font-mono text-xs text-muted-foreground hover:text-foreground sm:block">
                      {w.task_id}
                    </Link>
                    <span className="min-w-0">
                      <span className="block truncate font-medium">{titles.get(w.task_id) ?? w.task_id}</span>
                      <span className="block truncate text-xs text-muted-foreground">
                        {t("now.waiting_budget", { provider: w.provider, time: formatRelative(w.reset_at, now) })}
                      </span>
                    </span>
                  </li>
                ))}
              </ul>
            ) : (
              <p className="rounded-lg border border-dashed px-3.5 py-5 text-muted-foreground">{t("now.all_clear")}</p>
            )}
          </section>

          {idle && estimate && estimate.tasks.length > 0 ? <NextRun e={estimate} /> : null}
        </div>

        <aside aria-label={t("now.context")} className="min-w-0 space-y-6">
          <section aria-labelledby="now-phases">
            <Heading id="now-phases" title={t("now.phases")} />
            <ul className={cn(panel, "space-y-3 p-3.5")}>
              {(phases ?? []).map((m) => (
                <li key={m.phase} className="grid grid-cols-[minmax(0,7.5rem)_minmax(0,1fr)_3rem] items-center gap-3">
                  <span className="truncate" title={m.name}>
                    {m.name}
                  </span>
                  <Meter pct={m.progress.pct} tone={m.progress.pct >= 100 ? "bg-status-done" : "bg-brand"} label={m.name} />
                  <span className="text-right font-mono text-xs tabular-nums text-muted-foreground">
                    {m.progress.done}/{m.progress.total}
                  </span>
                </li>
              ))}
            </ul>
          </section>

          <section aria-labelledby="now-budget">
            <Heading
              id="now-budget"
              title={t("now.budget")}
              aside={
                <Link to="/cost/budget" className="underline-offset-4 hover:text-foreground hover:underline">
                  {t("now.budget_details")}
                </Link>
              }
            />
            {budget?.available && budget.rows.length > 0 ? (
              <ul className={cn(panel, "divide-y")}>
                {budget.rows.map((row) => (
                  <BudgetGauge key={row.provider} row={row} now={now} />
                ))}
              </ul>
            ) : (
              <p className="rounded-lg border border-dashed px-3.5 py-5 text-muted-foreground">{t("now.budget_off")}</p>
            )}
          </section>

          {receipt && r ? (
            <section aria-labelledby="now-last-run">
              <Heading
                id="now-last-run"
                title={t("now.last_run")}
                aside={r.finished_at ? t("receipt.finished", { time: formatRelative(r.finished_at, now) }) : t("receipt.unfinished")}
              />
              <RunReceipt payload={receipt} />
            </section>
          ) : null}
        </aside>
      </div>
    </div>
  )
}
