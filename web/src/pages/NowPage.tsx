import { useState } from "react"
import { Link } from "react-router-dom"
import { AlertTriangle, ChevronDown, Hourglass, OctagonPause } from "lucide-react"
import { ActivityFeed } from "@/components/ActivityFeed"
import { TaskLink } from "@/pages/TaskPage"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { Skeleton } from "@/components/ui/skeleton"
import { CopyCommand } from "@/components/CopyCommand"
import { Explain } from "@/components/Explain"
import { FirstRunChecklist } from "@/components/FirstRunChecklist"
import { LiveStatusPill } from "@/components/LiveStatusPill"
import { RunReceipt } from "@/components/RunReceipt"
import { useBudgetSummary, type BudgetRow } from "@/hooks/useBudgetSummary"
import { useOnboarding, useReceipt } from "@/hooks/useReceipt"
import { useEventStream } from "@/hooks/useEventStream"
import { useMilestones } from "@/hooks/useMilestones"
import { useEstimate, useNow, useTicker, type Estimate, type NowDispatch } from "@/hooks/useNow"
import { useTasks } from "@/hooks/useTasks"
import { fmt, t } from "@/i18n"
import { describeLoadError } from "@/lib/errors"
import { STATUS } from "@/lib/status"
import { formatDuration, formatElapsed, formatRelative } from "@/lib/time"
import { cn } from "@/lib/utils"
import { PhaseSegments } from "@/pages/StakeholderSummaryPage"
import type { SprintBlocker } from "@/lib/types"

// The operator's first screen. Three questions, in the order they matter:
// what is running this minute, what needs a person, and how the project and
// its budget are pacing. Everything else is one destination away.

// Which sections the operator folded, remembered per browser: the page is
// watched for hours, and a section folded once should stay folded.
const COLLAPSED_KEY = "orch_now_collapsed"

function readCollapsed(): Set<string> {
  try {
    const v: unknown = JSON.parse(window.localStorage.getItem(COLLAPSED_KEY) ?? "[]")
    return new Set(Array.isArray(v) ? v.filter((x): x is string => typeof x === "string") : [])
  } catch {
    return new Set()
  }
}

function useCollapsed(): [Set<string>, (id: string) => void] {
  const [collapsed, setCollapsed] = useState(readCollapsed)
  const toggle = (id: string) =>
    setCollapsed((prev) => {
      const next = new Set(prev)
      if (!next.delete(id)) next.add(id)
      try {
        window.localStorage.setItem(COLLAPSED_KEY, JSON.stringify([...next]))
      } catch {
        // storage blocked: the fold still works for this visit
      }
      return next
    })
  return [collapsed, toggle]
}

// A section whose heading folds it. The aside stays visible while folded, so
// a folded "Needs your attention" still shows its count.
function Section({
  id,
  title,
  aside,
  collapsed,
  onToggle,
  children,
}: {
  id: string
  title: React.ReactNode
  aside?: React.ReactNode
  collapsed: boolean
  onToggle: () => void
  children: React.ReactNode
}) {
  return (
    <section aria-labelledby={id}>
      <div className={cn("flex items-baseline justify-between gap-3", !collapsed && "mb-3")}>
        <h2 id={id} className="text-sm font-semibold tracking-[-0.01em]">
          <button
            type="button"
            onClick={onToggle}
            aria-expanded={!collapsed}
            aria-controls={`${id}-body`}
            className="inline-flex items-center gap-1.5 rounded-sm hover:text-foreground/80 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          >
            <ChevronDown
              className={cn("h-3.5 w-3.5 text-muted-foreground transition-transform motion-reduce:transition-none", collapsed && "-rotate-90")}
              aria-hidden
            />
            {title}
          </button>
        </h2>
        {aside ? <span className="text-xs text-muted-foreground tabular-nums">{aside}</span> : null}
      </div>
      <div id={`${id}-body`} hidden={collapsed}>
        {collapsed ? null : children}
      </div>
    </section>
  )
}

// An agent at work; opening the row follows what it is doing, live — the
// work a dispatched agent does is otherwise invisible until it ends.
function WorkingRow({ d, now }: { d: NowDispatch; now: number }) {
  const [open, setOpen] = useState(false)
  const started = new Date(d.started_at).getTime()
  const attempt =
    d.max_attempts > 0 ? t("now.attempt", { attempt: d.attempt, max: d.max_attempts }) : t("now.attempt_n", { attempt: d.attempt })
  const activityId = `activity-${d.task_id}`
  return (
    <li>
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        aria-expanded={open}
        aria-controls={activityId}
        title={t("activity.show", { id: d.task_id })}
        className="grid w-full grid-cols-[minmax(0,1fr)_auto] items-center gap-3 px-4 py-3 text-left hover:bg-accent/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring sm:grid-cols-[4.5rem_minmax(0,1fr)_auto]"
      >
        <span className="hidden font-mono text-xs text-muted-foreground sm:block">{d.task_id}</span>
        <span className="min-w-0">
          <span className="block truncate text-sm font-medium">{d.title}</span>
          <span className="block truncate font-mono text-[11px] text-muted-foreground">
            <span className="sm:hidden">{d.task_id} · </span>
            {[d.provider, d.model, d.attempt > 1 || d.max_attempts > 0 ? attempt : null].filter(Boolean).join(" · ")}
          </span>
        </span>
        <span
          className="inline-flex items-center gap-2 font-mono text-sm tabular-nums text-status-running"
          title={Number.isNaN(started) ? undefined : fmt.dateTime(d.started_at)}
          aria-label={Number.isNaN(started) ? undefined : t("now.elapsed", { elapsed: formatElapsed(now - started) })}
        >
          <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-status-running motion-reduce:animate-none" aria-hidden />
          {Number.isNaN(started) ? "—" : formatElapsed(now - started)}
          <ChevronDown
            className={cn("h-3.5 w-3.5 text-muted-foreground transition-transform motion-reduce:transition-none", open && "rotate-180")}
            aria-hidden
          />
        </span>
      </button>
      <div id={activityId} hidden={!open} className="border-t bg-muted/30 px-4 py-3">
        {open ? (
          <>
            <TaskLink id={d.task_id} className="mb-2 inline-block text-xs text-primary underline-offset-4 hover:underline">
              {t("task.open_page", { id: d.task_id })}
            </TaskLink>
            <ActivityFeed taskId={d.task_id} live provider={d.provider} />
          </>
        ) : null}
      </div>
    </li>
  )
}

function BlockedRow({ b, now }: { b: SprintBlocker; now: number }) {
  return (
    <li className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-3 px-4 py-3 sm:grid-cols-[4.5rem_minmax(0,1fr)_auto]">
      <span className="hidden font-mono text-xs text-muted-foreground sm:block">{b.task_id}</span>
      <span className="min-w-0">
        <span className="flex items-center gap-2">
          <OctagonPause className={cn("h-3.5 w-3.5 shrink-0", STATUS.blocked.text)} aria-label={t("status.blocked")} />
          <span className="truncate text-sm font-medium">{b.title}</span>
        </span>
        <span className="mt-0.5 block truncate text-xs text-muted-foreground" title={b.reason}>
          <span className="font-mono sm:hidden">{b.task_id} · </span>
          {b.reason}
          {b.blocked_at ? ` · ${t("now.blocked_since", { time: formatRelative(b.blocked_at, now) })}` : null}
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

function BudgetGauge({ row, now }: { row: BudgetRow; now: number }) {
  const pct = row.token_budget > 0 ? Math.min(100, (row.tokens_used / row.token_budget) * 100) : 0
  const tone = row.over_threshold ? "bg-status-blocked" : "bg-brand"
  const source = row.cost_source === "none" ? null : t(`now.cost.${row.cost_source}`)
  return (
    <li className="px-4 py-3">
      <div className="flex items-baseline justify-between gap-3 text-sm">
        <span className="font-medium">{row.provider}</span>
        <span className="font-mono text-xs tabular-nums text-muted-foreground">
          {t("now.tokens", { used: fmt.number(row.tokens_used), budget: fmt.number(row.token_budget) })}
        </span>
      </div>
      <div
        className="mt-2 h-1.5 overflow-hidden rounded-full bg-muted"
        role="meter"
        aria-valuemin={0}
        aria-valuemax={row.token_budget}
        aria-valuenow={row.tokens_used}
        aria-label={row.provider}
      >
        <div className={cn("h-full rounded-full transition-[width]", tone)} style={{ width: `${pct}%` }} />
      </div>
      <p className="mt-1.5 flex flex-wrap gap-x-2 text-[11px] text-muted-foreground">
        <span>{t("now.window", { hours: row.window_hours })}</span>
        {row.raw_tokens_used !== row.tokens_used ? <span>· {t("now.tokens_raw", { raw: fmt.number(row.raw_tokens_used) })}</span> : null}
        {row.reset_at ? <span>· {t("now.resets", { time: formatRelative(row.reset_at, now) })}</span> : null}
        {source ? (
          <span>
            · {fmt.usd(row.cost_source === "estimated" ? row.estimated_cost_usd : row.cost_usd)} {source}
          </span>
        ) : null}
      </p>
    </li>
  )
}

// What the next `orch run` would take, shown while nothing runs (#357).
function NextRun({ e }: { e: Estimate }) {
  return (
    <p className="mt-2 text-sm">
      <span className="tabular-nums">
        {t("now.next_run", { count: e.tasks.length, cost: fmt.usd(e.cost_usd), time: formatDuration(e.wall_s), slots: e.slots })}
      </span>
      <span className="mt-0.5 block text-xs text-muted-foreground">
        {e.confidence === "none"
          ? t("now.next_run_rough")
          : t("now.next_run_basis", { count: e.past_attempts, confidence: t(`now.confidence.${e.confidence}`) })}
      </span>
    </p>
  )
}

const panel = "overflow-hidden rounded-xl border bg-card"

export function NowPage() {
  const now = useTicker()
  const { data, isLoading, isError, error } = useNow()
  const { status: streamStatus, lastEventAt } = useEventStream()
  const { data: phases } = useMilestones()
  const { data: budget } = useBudgetSummary()
  const { data: tasks } = useTasks()
  const { data: receipt } = useReceipt()
  const onboarding = useOnboarding()
  const idle = !!data && data.working.length === 0 && data.run?.status !== "live"
  const { data: estimate } = useEstimate(idle)
  const [collapsed, toggle] = useCollapsed()
  const fold = (id: string) => ({ id, collapsed: collapsed.has(id), onToggle: () => toggle(id) })

  if (isLoading) {
    return (
      <div className="grid gap-6 xl:grid-cols-[minmax(0,1.6fr)_minmax(0,1fr)]">
        <div className="space-y-4">
          <Skeleton className="h-20 w-full" />
          <Skeleton className="h-48 w-full" />
        </div>
        <Skeleton className="h-72 w-full" />
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

  return (
    <div className="space-y-8">
      <header className="flex flex-wrap items-start justify-between gap-4">
        <div className="min-w-0">
          <h1 className="text-2xl font-semibold tracking-[-0.02em]">{data.project || t("now.title")}</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            {run
              ? run.status === "live"
                ? t("now.run_live", { id: run.run_id, time: formatRelative(run.started_at, now) })
                : t("now.run_done", { id: run.run_id, time: formatRelative(run.updated_at || run.started_at, now) })
              : null}
          </p>
        </div>
        <LiveStatusPill status={streamStatus} lastEventAt={lastEventAt} />
      </header>

      <div className="grid gap-8 xl:grid-cols-[minmax(0,1.6fr)_minmax(0,1fr)]">
        <div className="min-w-0 space-y-8">
          {/* The run line: progress, who is working, and when it lands. */}
          <section aria-label={t("now.title")} className="flex flex-wrap items-baseline gap-x-5 gap-y-2">
            <p className="flex items-baseline gap-2">
              <span className="text-4xl font-semibold tracking-[-0.03em] tabular-nums">
                {t("now.tasks_done", { done: summary.done, total: summary.total })}
              </span>
              <span className="text-sm text-muted-foreground">{t("now.tasks_label")}</span>
            </p>
            <p className="text-sm text-muted-foreground">
              {data.working.length > 0 ? t("now.agents_working", { count: data.working.length }) : t("now.no_agents")}
              {" · "}
              {pace.eta_date ? (
                <Explain term="eta">
                  <span>
                    {t("now.eta", { date: fmt.day(pace.eta_date) })}
                    {pace.blocked_count > 0 ? ` (${t("now.eta_excludes", { count: pace.blocked_count })})` : null}
                  </span>
                </Explain>
              ) : (
                t("now.no_eta")
              )}
            </p>
          </section>

          {onboarding.data && !onboarding.data.complete ? (
            <FirstRunChecklist
              onboarding={onboarding.data}
              onRecheck={() => void onboarding.refetch()}
              checking={onboarding.isFetching}
            />
          ) : !run ? (
            <p className="text-sm text-muted-foreground">
              {t("now.never_ran")} <code className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs">orch run</code>
            </p>
          ) : null}

          <Section
            {...fold("now-working")}
            title={t("now.working")}
            aside={
              data.slots.max > 0
                ? t("now.slots", { used: data.slots.used, max: data.slots.max })
                : t("now.slots_unknown", { used: data.slots.used, count: data.slots.used })
            }
          >
            {data.working.length > 0 ? (
              <ul className={cn(panel, "divide-y")}>
                {data.working.map((d) => (
                  <WorkingRow key={`${d.task_id}-${d.started_at}`} d={d} now={now} />
                ))}
              </ul>
            ) : (
              <div className="rounded-xl border border-dashed px-4 py-6">
                <p className="text-sm text-muted-foreground">{t("now.nothing_running")}</p>
                {idle && estimate && estimate.tasks.length > 0 ? <NextRun e={estimate} /> : null}
              </div>
            )}
          </Section>

          <Section {...fold("now-attention")} title={t("now.attention")} aside={attentionCount > 0 ? attentionCount : undefined}>
            {attentionCount > 0 ? (
              <ul className={cn(panel, "divide-y")}>
                {data.attention.map((b) => (
                  <BlockedRow key={b.task_id} b={b} now={now} />
                ))}
                {data.waiting_for_budget.map((w) => (
                  <li key={w.task_id} className="grid grid-cols-1 items-center gap-3 px-4 py-3 sm:grid-cols-[4.5rem_minmax(0,1fr)]">
                    <span className="hidden font-mono text-xs text-muted-foreground sm:block">{w.task_id}</span>
                    <span className="min-w-0">
                      <span className="flex items-center gap-2">
                        <Hourglass className={cn("h-3.5 w-3.5 shrink-0", STATUS.blocked.text)} aria-hidden />
                        <span className="truncate text-sm font-medium">{titles.get(w.task_id) ?? w.task_id}</span>
                      </span>
                      <span className="mt-0.5 block truncate text-xs text-muted-foreground">
                        {t("now.waiting_budget", { provider: w.provider, time: formatRelative(w.reset_at, now) })}
                      </span>
                    </span>
                  </li>
                ))}
              </ul>
            ) : (
              <p className="rounded-xl border border-dashed px-4 py-6 text-sm text-muted-foreground">{t("now.all_clear")}</p>
            )}
          </Section>
        </div>

        <div className="min-w-0 space-y-8">
          {receipt?.receipt ? (
            <Section {...fold("now-last-run")} title={t("now.last_run")}>
              <RunReceipt payload={receipt} now={now} />
            </Section>
          ) : null}

          <Section {...fold("now-phases")} title={t("now.phases")}>
            <ul className={cn(panel, "space-y-3 p-4")}>
              {(phases ?? []).map((m) => (
                <li key={m.phase} className="grid grid-cols-[minmax(0,7.5rem)_minmax(0,1fr)_3rem] items-center gap-3">
                  <span className="truncate text-sm" title={m.name}>
                    {m.name}
                  </span>
                  <PhaseSegments
                    phase={{
                      phase: m.phase,
                      name: m.name,
                      total: m.progress.total,
                      done: m.progress.done,
                      in_progress: m.in_progress,
                      blocked: m.blocked,
                      backlog: 0,
                      estimate_hours: 0,
                      pct_done: m.progress.pct,
                    }}
                  />
                  <span className="text-right font-mono text-xs tabular-nums text-muted-foreground">
                    {m.progress.done}/{m.progress.total}
                  </span>
                </li>
              ))}
            </ul>
          </Section>

          <Section
            {...fold("now-budget")}
            title={budget?.preset ? `${t("now.budget")} · ${budget.preset}` : t("now.budget")}
            aside={
              <Link to="/cost/budget" className="underline-offset-4 hover:text-foreground hover:underline">
                {t("now.budget_details")}
              </Link>
            }
          >
            {budget?.available && budget.rows.length > 0 ? (
              <ul className={cn(panel, "divide-y")}>
                {budget.rows.map((row) => (
                  <BudgetGauge key={row.provider} row={row} now={now} />
                ))}
              </ul>
            ) : (
              <p className="rounded-xl border border-dashed px-4 py-6 text-sm text-muted-foreground">{t("now.budget_off")}</p>
            )}
          </Section>
        </div>
      </div>
    </div>
  )
}
