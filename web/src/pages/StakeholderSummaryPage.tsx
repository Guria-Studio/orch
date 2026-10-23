import { useState } from "react"
import { formatEta } from "@/lib/eta"
import { useWhoami } from "@/hooks/useWhoami"
import { AlertTriangle, Check, Copy, Download, Eye, Loader2 } from "lucide-react"
import { Link } from "react-router-dom"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button, buttonVariants } from "@/components/ui/button"
import { Progress } from "@/components/ui/progress"
import { Skeleton } from "@/components/ui/skeleton"
import { Explain } from "@/components/Explain"
import { ProjectConfigWidget } from "@/components/ProjectConfigWidget"
import { isStakeholderSummaryUnavailable, useStakeholderSummary } from "@/hooks/useStakeholderSummary"
import { fmt, t } from "@/i18n"
import { STATUS, type StatusKey } from "@/lib/status"
import { cn } from "@/lib/utils"
import type { SpendByDay, StakeholderMilestone, StakeholderPhase } from "@/lib/types"

// ---- Phases ----------------------------------------------------------------

/**
 * One segment per task, colored by status, so a phase reads as "how many of
 * its tasks are where" — length never doubles as effort.
 */
export function PhaseSegments({ phase }: { phase: StakeholderPhase }) {
  const todo = Math.max(0, phase.total - phase.done - phase.in_progress - phase.blocked)
  const segments: StatusKey[] = [
    ...Array<StatusKey>(phase.done).fill("done"),
    ...Array<StatusKey>(phase.in_progress).fill("in_progress"),
    ...Array<StatusKey>(phase.blocked).fill("blocked"),
    ...Array<StatusKey>(todo).fill("todo"),
  ]
  return (
    <div
      className="flex h-2.5 min-w-0 flex-1 gap-[3px]"
      role="img"
      aria-label={t("summary.phase_segments", {
        done: phase.done,
        active: phase.in_progress,
        blocked: phase.blocked,
        total: phase.total,
      })}
    >
      {segments.map((key, i) => (
        <span
          key={i}
          className={cn("h-full min-w-[3px] flex-1 rounded-[2px]", key === "todo" ? "bg-muted" : STATUS[key].fill)}
        />
      ))}
    </div>
  )
}

/** One bar per phase, stacked done / in progress / blocked, the rest the track. */
function PhaseBars({ phases }: { phases: StakeholderPhase[] }) {
  const legend: { label: string; className: string }[] = [
    { label: t("status.done"), className: STATUS.done.fill },
    { label: t("status.in_progress"), className: STATUS.in_progress.fill },
    { label: t("status.blocked"), className: STATUS.blocked.fill },
    { label: t("status.backlog"), className: "bg-muted" },
  ]
  return (
    <section className="space-y-3 rounded-lg border bg-card p-4" aria-labelledby="phases-heading">
      <h2 id="phases-heading" className="text-xs font-medium text-muted-foreground">
        {t("summary.phases")}
      </h2>
      <ul className="space-y-3">
        {phases.map((phase) => {
          const pct = (n: number) => (phase.total > 0 ? `${(n / phase.total) * 100}%` : "0%")
          return (
            <li key={phase.phase} className="space-y-1.5">
              <div className="flex items-baseline justify-between gap-2">
                <span className="truncate font-medium" title={phase.name}>
                  {phase.name}
                </span>
                <span className="shrink-0 text-xs tabular-nums text-muted-foreground">
                  {t("summary.tasks_of", { done: phase.done, total: phase.total })}
                </span>
              </div>
              <div
                className="flex h-2 overflow-hidden rounded-full bg-muted"
                role="img"
                aria-label={t("summary.phase_segments", {
                  done: phase.done,
                  active: phase.in_progress,
                  blocked: phase.blocked,
                  total: phase.total,
                })}
              >
                <span className={STATUS.done.fill} style={{ width: pct(phase.done) }} />
                <span className={STATUS.in_progress.fill} style={{ width: pct(phase.in_progress) }} />
                <span className={STATUS.blocked.fill} style={{ width: pct(phase.blocked) }} />
              </div>
            </li>
          )
        })}
      </ul>
      <p className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
        {legend.map((l) => (
          <span key={l.label} className="inline-flex items-center gap-1.5">
            <span className={cn("h-2 w-2 rounded-[2px]", l.className)} />
            {l.label}
          </span>
        ))}
      </p>
    </section>
  )
}

/** Each milestone with where it stands: done, in progress, or not started. */
function Milestones({ milestones, phases }: { milestones: StakeholderMilestone[]; phases: StakeholderPhase[] }) {
  const nameOf = new Map(phases.map((p) => [p.phase, p.name]))
  return (
    <section className="space-y-3 rounded-lg border bg-card p-4" aria-labelledby="milestones-heading">
      <h2 id="milestones-heading" className="text-xs font-medium text-muted-foreground">
        {t("summary.milestones")}
      </h2>
      <ul className="space-y-2.5">
        {milestones.map((m) => {
          const state = m.done ? "done" : m.done_count > 0 ? "active" : "pending"
          const dot = state === "done" ? STATUS.done.fill : state === "active" ? STATUS.in_progress.fill : "bg-muted-foreground/50"
          return (
            <li key={m.phase} className="flex items-center gap-2.5">
              <span className={cn("h-2 w-2 shrink-0 rounded-full", dot)} aria-hidden />
              <span className="min-w-0 flex-1 truncate font-medium">{nameOf.get(m.phase) ?? t("filters.phase", { phase: m.phase })}</span>
              <span className="shrink-0 text-xs text-muted-foreground">
                {t(`milestones.status.${state}` as const)}
                {state === "active" ? ` · ${t("summary.tasks_of", { done: m.done_count, total: m.total_count })}` : null}
              </span>
            </li>
          )
        })}
      </ul>
    </section>
  )
}

// ---- Spend -----------------------------------------------------------------

// total is spend_rounded_usd when the server sent it: the same figure the
// executive summary quotes, so the card and the sentence cannot disagree
// ($185.76 next to "$186.00").
function Spend({ days, total: sharedTotal, showNote }: { days: SpendByDay[]; total: number | null; showNote: boolean }) {
  const max = Math.max(...days.map((d) => d.cost), 0.01)
  const total = sharedTotal ?? days.reduce((s, d) => s + d.cost, 0)
  return (
    <section className="space-y-3 rounded-lg border bg-card p-4" aria-labelledby="spend-heading">
      <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
        <h2 id="spend-heading" className="text-xs font-medium text-muted-foreground">
          <Explain term="spend">{t("summary.spend")}</Explain>
        </h2>
        {showNote ? <span className="text-xs text-muted-foreground">{t("summary.spend_note")}</span> : null}
        <span className="ml-auto text-lg font-semibold tabular-nums">{fmt.usd(total)}</span>
      </div>
      {days.length > 1 ? (
        <>
          <div className="flex h-16 items-end gap-1" role="img" aria-label={t("summary.spend_chart")}>
            {days.map((d, i) => (
              <span
                key={d.date}
                title={`${fmt.day(d.date)}: ${fmt.usd(d.cost)}`}
                className={cn("min-w-0 flex-1 rounded-t-[2px]", i === days.length - 1 ? "bg-brand" : "bg-brand/40")}
                style={{ height: `${Math.max(3, (d.cost / max) * 100)}%` }}
              />
            ))}
          </div>
          <div className="flex justify-between text-xs tabular-nums text-muted-foreground">
            <span>{fmt.day(days[0].date)}</span>
            <span>{fmt.day(days[days.length - 1].date)}</span>
          </div>
        </>
      ) : null}
    </section>
  )
}

// ---- Executive summary -----------------------------------------------------

function ExecSummary({ text }: { text: string }) {
  const [copied, setCopied] = useState(false)
  if (!text) return null
  const handleCopy = () => {
    void navigator.clipboard.writeText(text).then(() => {
      setCopied(true)
      window.setTimeout(() => setCopied(false), 2000)
    })
  }
  return (
    <section className="rounded-lg border bg-card p-4" aria-labelledby="exec-heading">
      <div className="flex items-center justify-between gap-2">
        <h2 id="exec-heading" className="text-xs font-medium text-muted-foreground">
          {t("summary.exec")}
        </h2>
        <Button variant="outline" size="sm" className="h-8 gap-1.5 print:hidden" onClick={handleCopy}>
          {copied ? <Check className={cn("h-3.5 w-3.5", STATUS.done.text)} /> : <Copy className="h-3.5 w-3.5" />}
          {copied ? t("common.copied") : t("common.copy")}
        </Button>
      </div>
      <p className="mt-2 max-w-[75ch] whitespace-pre-line text-sm leading-relaxed text-muted-foreground">{text}</p>
    </section>
  )
}

// ---- Main page -------------------------------------------------------------

export function StakeholderSummaryPage() {
  const { data, isLoading, isError, error, isFetching } = useStakeholderSummary()
  const { data: whoami } = useWhoami()
  const isStakeholder = whoami?.profile === "stakeholder"

  if (isLoading) {
    return (
      <div className="space-y-6">
        <Skeleton className="h-8 w-72" />
        <Skeleton className="h-24 w-full" />
        <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
          {[...Array(4)].map((_, i) => (
            <Skeleton key={i} className="h-24" />
          ))}
        </div>
        <Skeleton className="h-40 w-full" />
      </div>
    )
  }

  if (isError) {
    // Bug 26's own review (PR #205) found this: the Go dashboard doesn't
    // implement /stakeholder/summary yet, so this named state is the
    // COMMON case today, not a rare one — a plain "failed to load"
    // alert would read as a transient problem worth retrying, when the
    // real answer is "this binary doesn't have this page yet."
    if (isStakeholderSummaryUnavailable(error)) {
      return (
        <Alert>
          <AlertTriangle className="h-4 w-4" />
          <AlertTitle>{t("summary.unavailable")}</AlertTitle>
          <AlertDescription>{t("summary.unavailable_detail")}</AlertDescription>
        </Alert>
      )
    }
    return (
      <Alert variant="destructive">
        <AlertTriangle className="h-4 w-4" />
        <AlertTitle>{t("summary.load_failed")}</AlertTitle>
        <AlertDescription>{error?.message ?? t("common.unknown_error")}</AlertDescription>
      </Alert>
    )
  }

  if (!data) return null

  const { project_id, summary, milestones, spend_rounded_usd, refresh_interval_s, phases_timeline, spend_by_day, exec_summary } =
    data
  const eta = formatEta(data)
  const refreshSeconds = refresh_interval_s && refresh_interval_s > 0 ? refresh_interval_s : 10
  const phases = phases_timeline ?? []

  return (
    <div className="space-y-4 text-[13px]">
      {/* The layout's breadcrumb names the page on screen; a printed PDF has none, so it carries the project. */}
      <h1 className="hidden text-xl font-semibold print:block">{project_id}</h1>
      <div className="flex flex-wrap items-center justify-end gap-2 print:hidden">
        <span className="mr-auto inline-flex items-center gap-2 text-xs text-muted-foreground">
          {t("summary.refresh", { seconds: refreshSeconds })}
          {isFetching ? <Loader2 className="h-3.5 w-3.5 animate-spin" aria-label={t("common.refreshing")} /> : null}
        </span>
        <Button variant="outline" size="sm" className="gap-1.5" onClick={() => window.print()}>
          <Download className="h-3.5 w-3.5" />
          {t("summary.export_pdf")}
        </Button>
        {isStakeholder ? null : (
          <Link to="/delivery/client" className={cn(buttonVariants({ size: "sm" }), "gap-1.5")}>
            <Eye className="h-3.5 w-3.5" />
            {t("summary.view_as_client")}
          </Link>
        )}
      </div>

      <section className="space-y-3 rounded-lg border bg-card p-4" aria-labelledby="progress-heading">
        <h2 id="progress-heading" className="text-xs font-medium text-muted-foreground">
          {t("summary.progress")}
        </h2>
        <div className="flex flex-wrap items-baseline gap-x-4 gap-y-1">
          <span className="text-2xl font-semibold tabular-nums tracking-[-0.02em]">{Math.round(summary.percent_done)}%</span>
          <span className="tabular-nums text-muted-foreground">{t("summary.tasks_done", { done: summary.done, total: summary.total })}</span>
          <span className="tabular-nums text-muted-foreground">
            <Explain term="estimate">{t("summary.effort", { hours: summary.estimate_hours_total })}</Explain>
          </span>
          {eta.value !== "—" ? (
            <span className="tabular-nums text-muted-foreground">
              <Explain term="eta" /> {eta.value}
              {eta.detail ? ` ${eta.detail}` : null}
            </span>
          ) : null}
        </div>
        <Progress value={summary.percent_done} className="h-2" />
      </section>

      {exec_summary ? <ExecSummary text={exec_summary} /> : null}

      {phases.length > 0 || milestones.length > 0 ? (
        <div className="grid gap-4 lg:grid-cols-2">
          {phases.length > 0 ? <PhaseBars phases={phases} /> : null}
          {milestones.length > 0 ? <Milestones milestones={milestones} phases={phases} /> : null}
        </div>
      ) : null}

      {spend_rounded_usd == null && !(spend_by_day?.length > 1) ? null : (
        <Spend days={spend_by_day ?? []} total={spend_rounded_usd} showNote={!isStakeholder} />
      )}

      {/* /api/config is operator material, and a stakeholder token gets 403 on it. */}
      {isStakeholder ? null : <ProjectConfigWidget />}
    </div>
  )
}
