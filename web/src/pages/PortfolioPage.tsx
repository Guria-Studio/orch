import { AlertTriangle } from "lucide-react"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Progress } from "@/components/ui/progress"
import { Skeleton } from "@/components/ui/skeleton"
import { isPortfolioDisabled, usePortfolio } from "@/hooks/usePortfolio"
import { useTicker } from "@/hooks/useNow"
import { useWhoami } from "@/hooks/useWhoami"
import type { PortfolioProject } from "@/lib/types"
import { fmt, t } from "@/i18n"
import { formatRelative } from "@/lib/time"
import { cn } from "@/lib/utils"

/**
 * G8.5 — several projects, one screen. Each row is deliberately compact
 * (name, progress, ETA/confidence, blockers, spend, last event, all on
 * one line at rest) so that five projects — the stated acceptance
 * criterion — fit without scrolling on a normal laptop screen; the
 * per-project detail this row summarizes already has its own full pages
 * (Sprint, Budget, Kanban) one click away at `/p/<project_id>/`.
 *
 * Operator-only and gated on the dashboard having been started with
 * `--portfolio` — AppLayout hides the nav entry for both reasons before
 * this page is ever reached, but a direct link (bookmark, typed URL)
 * still needs its own answer. `enabled: !isStakeholder` below is what
 * actually keeps that promise for a stakeholder who lands here directly
 * — without it, usePortfolio's own doc comment ("a stakeholder session
 * should never even issue the request") would be true only via
 * AppLayout, not here, which is exactly the gap opus's review of this
 * PR found.
 */
export function PortfolioPage() {
  const { data: whoami } = useWhoami()
  const isStakeholder = whoami?.profile === "stakeholder"
  const { data, isLoading, isError, error } = usePortfolio({ enabled: !isStakeholder })
  const now = useTicker(60_000)

  if (isLoading) {
    return (
      <div className="space-y-4">
        <h1 className="text-xl font-semibold tracking-[-0.01em]">{t("portfolio.title")}</h1>
        {[1, 2, 3].map((i) => (
          <Skeleton key={i} className="h-20 w-full" />
        ))}
      </div>
    )
  }

  if (isError) {
    if (isPortfolioDisabled(error)) {
      return (
        <Alert>
          <AlertTriangle className="h-4 w-4" />
          <AlertTitle>{t("portfolio.off")}</AlertTitle>
          <AlertDescription>
            {t("portfolio.off_before")} <code className="font-mono">--portfolio</code>. {t("portfolio.off_after")}
          </AlertDescription>
        </Alert>
      )
    }
    return (
      <Alert variant="destructive">
        <AlertTriangle className="h-4 w-4" />
        <AlertTitle>{t("portfolio.load_failed")}</AlertTitle>
        <AlertDescription>{error?.message ?? t("common.unknown_error")}</AlertDescription>
      </Alert>
    )
  }

  if (!data) return null

  const down = data.projects.filter((p) => !p.available).length
  const cols = "sm:grid-cols-[minmax(0,1.4fr)_minmax(0,1.2fr)_minmax(0,1fr)_7rem_6rem_8rem]"

  return (
    <div className="space-y-4 text-[13px]">
      <header className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
        <h1 className="text-xl font-semibold tracking-[-0.01em]">{t("portfolio.title")}</h1>
        <p className="ml-auto flex flex-wrap gap-x-3 text-xs text-muted-foreground tabular-nums">
          <span>{t("portfolio.projects", { count: data.projects.length })}</span>
          {down > 0 ? <span className="text-status-blocked">{t("portfolio.unavailable", { count: down })}</span> : null}
          {data.unavailable.length > 0 ? <span>{t("portfolio.skipped", { count: data.unavailable.length })}</span> : null}
        </p>
      </header>

      <div className="overflow-hidden rounded-lg border bg-card">
        <div className={cn("hidden gap-x-4 border-b px-3.5 py-2 text-xs text-muted-foreground sm:grid", cols)}>
          <span>{t("portfolio.col.project")}</span>
          <span>{t("portfolio.col.progress")}</span>
          <span>{t("portfolio.col.eta")}</span>
          <span>{t("portfolio.col.blockers")}</span>
          <span className="text-right">{t("portfolio.col.spend")}</span>
          <span>{t("portfolio.col.last")}</span>
        </div>
        <ul className="divide-y">
          {data.projects.map((p) => (
            <ProjectRow key={p.project_id} project={p} cols={cols} now={now} />
          ))}
        </ul>
        {data.unavailable.length > 0 ? (
          <details className="border-t">
            <summary className="cursor-pointer select-none px-3.5 py-2.5 text-xs text-muted-foreground">
              {t("portfolio.skipped", { count: data.unavailable.length })}
            </summary>
            <ul className="space-y-1 px-3.5 pb-3 font-mono text-xs text-muted-foreground">
              {data.unavailable.map((u) => (
                <li key={u.root} className="flex gap-1.5 truncate">
                  <span>{u.root}</span>—<span>{u.reason}</span>
                </li>
              ))}
            </ul>
          </details>
        ) : null}
      </div>
    </div>
  )
}

function ConfidenceDot({ confidence }: { confidence: PortfolioProject["confidence"] }) {
  if (confidence === "none") return null
  const label = confidence === "high" ? t("portfolio.high_confidence") : t("portfolio.low_confidence")
  return (
    <span
      role="img"
      aria-label={label}
      title={label}
      className={cn("inline-block h-1.5 w-1.5 shrink-0 rounded-full", confidence === "high" ? "bg-status-done" : "bg-status-blocked")}
    />
  )
}

function ProjectRow({ project, cols, now }: { project: PortfolioProject; cols: string; now: number }) {
  if (!project.available) {
    return (
      <li className={cn("grid gap-x-4 gap-y-1 px-3.5 py-2.5", cols)}>
        <span className="min-w-0">
          <span className="block truncate font-medium text-muted-foreground">{project.project_name}</span>
          <span className="block truncate font-mono text-xs text-muted-foreground">{project.root}</span>
        </span>
        <span className="self-center text-xs text-status-failed sm:col-span-5">
          {t("portfolio.unavailable_one")}
          {project.reason ? (
            <>
              {" · "}
              <span>{project.reason}</span>
            </>
          ) : null}
        </span>
      </li>
    )
  }

  const blocked = project.blocked > 0
  return (
    <li>
      <a href={`/p/${encodeURIComponent(project.project_id)}/`} className={cn("grid items-center gap-x-4 gap-y-1.5 px-3.5 py-2.5 hover:bg-accent/40", cols)}>
        <span className="min-w-0">
          <span className="block truncate font-medium">{project.project_name}</span>
          <span className="block truncate font-mono text-xs text-muted-foreground">{project.root}</span>
        </span>
        <span className="flex min-w-0 items-center gap-2">
          <Progress value={project.percent_done} className="h-1.5 min-w-0 flex-1" />
          <span className="shrink-0 text-xs tabular-nums text-muted-foreground">
            {project.done}/{project.total} · {project.percent_done.toFixed(0)}%
          </span>
        </span>
        <span className="flex items-center gap-1.5 text-xs">
          <ConfidenceDot confidence={project.confidence} />
          {project.eta_date ? (
            <span className="tabular-nums">{t("portfolio.eta", { date: fmt.day(project.eta_date) })}</span>
          ) : (
            <span className="text-muted-foreground">{t("portfolio.no_eta")}</span>
          )}
        </span>
        <span
          className={cn("justify-self-start rounded-md px-1.5 py-0.5 text-[11px]", blocked ? "bg-status-blocked/14 text-status-blocked" : "bg-secondary text-muted-foreground")}
          title={project.blockers[0] ? `${project.blockers[0].title}: ${project.blockers[0].reason}` : undefined}
        >
          {blocked ? t("portfolio.blocked", { count: project.blocked }) : t("portfolio.no_blockers")}
        </span>
        <span className="text-xs tabular-nums text-muted-foreground sm:text-right">
          {project.spend.available && project.spend.total_cost_usd !== undefined ? fmt.usd(project.spend.total_cost_usd) : t("portfolio.spend_hidden")}
        </span>
        {/* When the project last did anything; the event itself in the tooltip. */}
        <span
          className="truncate text-xs text-muted-foreground"
          title={project.last_event ? `${project.last_event.event_type}${project.last_event.task_id ? ` ${project.last_event.task_id}` : ""} · ${project.last_event.ts}` : undefined}
        >
          {project.last_event ? formatRelative(project.last_event.ts, now) : "—"}
        </span>
      </a>
    </li>
  )
}
