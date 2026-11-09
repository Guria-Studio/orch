import { useState } from "react"
import { Link } from "react-router-dom"
import { AlertTriangle, Eye, Play } from "lucide-react"
import { ActivityFeed } from "@/components/ActivityFeed"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Skeleton } from "@/components/ui/skeleton"
import { CopyCommand } from "@/components/CopyCommand"
import { Explain } from "@/components/Explain"
import { FirstRunChecklist } from "@/components/FirstRunChecklist"
import { RunReceipt } from "@/components/RunReceipt"
import { useBudgetSummary, type BudgetRow } from "@/hooks/useBudgetSummary"
import { useFindings } from "@/hooks/useFindings"
import { flowStep, useFlow } from "@/hooks/useFlow"
import { useOnboarding, useReceipt } from "@/hooks/useReceipt"
import { useMilestones } from "@/hooks/useMilestones"
import { useEstimate, useNow, useTicker, type Estimate, type NowDispatch, type NowWaiting } from "@/hooks/useNow"
import { useTasks } from "@/hooks/useTasks"
import { fmt, t } from "@/i18n"
import { moveTask } from "@/lib/api"
import { describeLoadError } from "@/lib/errors"
import { taskPath } from "@/lib/nav"
import { formatBytes, formatDuration, formatElapsed, formatRelative } from "@/lib/time"
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

const WAITING_KEYS = {
  download: "now.waiting.download",
  write: "now.waiting.write",
  read: "now.waiting.read",
  compute: "now.waiting.compute",
  edit: "now.waiting.edit",
} as const

/** What a silent agent is doing, from the watch's evidence (#524): "downloading data, +163 MB in 1h 0m". */
function waitingLine(w: NowWaiting): string {
  const vars = {
    size: formatBytes(w.activity === "read" ? w.read_bytes : w.files_bytes),
    time: formatDuration(w.window_s),
    cpu: formatDuration(w.cpu_s),
  }
  const key = WAITING_KEYS[w.activity] ?? "now.waiting.edit"
  return t("now.waiting", { what: t(key, vars) })
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
      {d.waiting ? (
        <p className="-mt-1.5 truncate px-3.5 pb-2.5 text-xs text-muted-foreground sm:pl-[calc(4.5rem+1.625rem)]" title={d.waiting.evidence}>
          {waitingLine(d.waiting)}
        </p>
      ) : null}
      <div id={activityId} hidden={!open} className="border-t bg-muted/30 px-3.5 py-3">
        {open ? <ActivityFeed taskId={d.task_id} live provider={d.provider} /> : null}
      </div>
    </li>
  )
}

function BlockedRow({ b, now }: { b: SprintBlocker; now: number }) {
  const [result, setResult] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  // The same move as `orch task set --status todo` (#549); the next run picks it up.
  const unblock = async () => {
    setBusy(true)
    try {
      setResult((await moveTask(b.task_id, "todo")).message)
    } catch (err) {
      setResult(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }
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
        {result ? (
          <span className="block truncate text-xs" role="status">
            {result}
          </span>
        ) : null}
      </span>
      <Button
        type="button"
        size="sm"
        variant="outline"
        disabled={busy}
        title={`orch task set --id ${b.task_id} --status todo`}
        onClick={() => void unblock()}
      >
        {t("now.unblock")}
      </Button>
    </li>
  )
}

/** A running agent the watch flagged silent, with the command that ends its attempt (#533). */
/** orch's own bugs its agents queued (#536): nothing leaves until the operator sends it. */
function FindingsBanner() {
  const { data } = useFindings()
  if (!data || data.pending === 0) return null
  return (
    <Alert>
      <AlertTriangle className="h-4 w-4" />
      <AlertTitle>{t("findings.banner", { count: data.pending })}</AlertTitle>
      <AlertDescription>
        <Link to="/system/findings" className="underline">
          {t("findings.banner_link")}
        </Link>
      </AlertDescription>
    </Alert>
  )
}

function StalledRow({ d }: { d: NowDispatch }) {
  return (
    <li className={cn(rowGrid, "grid-cols-1 sm:grid-cols-[4.5rem_minmax(0,1fr)]")}>
      <Link to={taskPath(d.task_id)} className="hidden font-mono text-xs text-muted-foreground hover:text-foreground sm:block">
        {d.task_id}
      </Link>
      <span className="min-w-0">
        <span className="block truncate font-medium">{d.title}</span>
        <span className="block truncate text-xs text-status-blocked">
          {t("task.stalled")} · {t("now.stalled")}
        </span>
        <CopyCommand command={`orch task stop --id ${d.task_id}`} />
      </span>
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

// What the next `orch run` would take, shown while nothing runs (#357), and
// the Run that starts it once the operator confirms that estimate (#548).
function NextRun({ e, canRun, onRan }: { e: Estimate; canRun: boolean; onRan: () => void }) {
  const [confirming, setConfirming] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const line = t("now.next_run", { count: e.tasks.length, cost: fmt.usd(e.cost_usd), time: formatDuration(e.wall_s), slots: e.slots })
  const run = async () => {
    setBusy(true)
    setError(null)
    try {
      await flowStep("run")
      setConfirming(false)
      onRan()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }
  return (
    <section aria-labelledby="now-next">
      <Heading id="now-next" title={t("now.next_title")} />
      <div className={cn(panel, "flex flex-wrap items-center gap-3 px-3.5 py-3")}>
        <div className="min-w-0 flex-1">
          <p className="tabular-nums">{line}</p>
          <p className="mt-0.5 text-xs text-muted-foreground">
            {e.confidence === "none"
              ? t("now.next_run_rough")
              : t("now.next_run_basis", { count: e.past_attempts, confidence: t(`now.confidence.${e.confidence}`) })}
          </p>
        </div>
        {canRun ? (
          <Button type="button" size="sm" onClick={() => setConfirming(true)} className="bg-brand text-white hover:bg-brand/90">
            <Play className="mr-1.5 h-3.5 w-3.5" aria-hidden />
            {t("now.run")}
          </Button>
        ) : null}
      </div>
      <Dialog open={confirming} onOpenChange={setConfirming}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("now.run_confirm_title")}</DialogTitle>
            <DialogDescription>{t("now.run_confirm_detail")}</DialogDescription>
          </DialogHeader>
          <p className="tabular-nums">{line}</p>
          {error ? <p className="whitespace-pre-wrap text-status-failed">{error}</p> : null}
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setConfirming(false)}>
              {t("common.cancel")}
            </Button>
            <Button type="button" disabled={busy} onClick={() => void run()} className="bg-brand text-white hover:bg-brand/90">
              {t("now.run_confirm", { count: e.tasks.length })}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
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
  const { data: flow, refetch: refetchFlow } = useFlow()

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
  const stalled = data.working.filter((d) => d.stalled)
  const attentionCount = data.attention.length + stalled.length + data.waiting_for_budget.length
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

      {flow?.running && run?.status !== "live" ? (
        <p className="flex items-center justify-end gap-2 text-xs text-muted-foreground" aria-live="polite">
          <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-status-running motion-reduce:animate-none" aria-hidden />
          {t("now.run_starting")}
        </p>
      ) : null}
      {flow?.run_error && !flow.running ? (
        <Alert variant="destructive">
          <AlertTriangle className="h-4 w-4" />
          <AlertTitle>{t("now.run_failed")}</AlertTitle>
          <AlertDescription className="font-mono text-xs">{flow.run_error}</AlertDescription>
        </Alert>
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

      <FindingsBanner />

      {summary.total === 0 ? (
        <p className="text-muted-foreground">
          {t("now.no_tasks")}{" "}
          <Link to="/work/plan" className="font-medium text-foreground underline underline-offset-4">
            {t("now.plan_link")}
          </Link>
        </p>
      ) : null}

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
                {stalled.map((d) => (
                  <StalledRow key={`stalled-${d.task_id}`} d={d} />
                ))}
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

          {idle && estimate && estimate.tasks.length > 0 ? (
            <NextRun e={estimate} canRun={!!flow && !flow.running} onRan={() => void refetchFlow()} />
          ) : null}
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
