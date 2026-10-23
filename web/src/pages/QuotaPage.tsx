import { useState } from "react"
import { AlertTriangle } from "lucide-react"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Skeleton } from "@/components/ui/skeleton"
import { useBudgetSummary, type BudgetRow } from "@/hooks/useBudgetSummary"
import { useQuota, type ProviderQuota, type QuotaWindow } from "@/hooks/useQuota"
import { fmt, t, type MessageKey } from "@/i18n"
import { formatRelative } from "@/lib/time"
import { cn } from "@/lib/utils"

/**
 * System › Providers and quota: one card per provider with the two numbers
 * that say how close it is to a limit — what the provider itself reported
 * about its subscription windows (claude's 5-hour and weekly, from its
 * latest dispatch), and orch's own budget window from budgets.yaml, which
 * pauses dispatches at its threshold. The first sees use outside orch too;
 * the second is the one `orch run` acts on.
 */
export function QuotaPage() {
  const quota = useQuota()
  const budget = useBudgetSummary()
  const loading = quota.isLoading || budget.isLoading
  const reported = quota.data?.providers ?? []
  const rows: BudgetRow[] = budget.data?.available ? budget.data.rows : []
  const names = [...new Set([...reported.map((q) => q.backend), ...rows.map((r) => r.provider)])].sort()

  return (
    <div className="max-w-5xl space-y-6 text-[13px]">
      <header className="space-y-1">
        <h1 className="text-xl font-semibold tracking-[-0.01em]">{t("quota.title")}</h1>
        <p className="max-w-3xl text-muted-foreground">{t("quota.intro")}</p>
      </header>

      {quota.isError ? (
        <Alert variant="destructive">
          <AlertTriangle className="h-4 w-4" />
          <AlertTitle>{t("quota.load_failed")}</AlertTitle>
          <AlertDescription>{quota.error?.message ?? t("common.unknown_error")}</AlertDescription>
        </Alert>
      ) : null}

      {loading ? (
        <Skeleton className="h-48 w-full" />
      ) : names.length === 0 ? (
        <p className="rounded-lg border border-dashed p-6 text-muted-foreground">{t("quota.none")}</p>
      ) : (
        <div className="grid gap-3 lg:grid-cols-2">
          {names.map((name) => (
            <ProviderCard key={name} name={name} quota={reported.find((q) => q.backend === name)} budget={rows.find((r) => r.provider === name)} />
          ))}
        </div>
      )}
    </div>
  )
}

function ProviderCard({ name, quota, budget }: { name: string; quota?: ProviderQuota; budget?: BudgetRow }) {
  // Read once per mount: the relative times need not tick.
  const [now] = useState(() => Date.now())
  return (
    <section aria-labelledby={`quota-${name}`} className="space-y-4 rounded-lg border bg-card p-3.5">
      <header className="flex items-center gap-2">
        <h2 id={`quota-${name}`} className="font-mono text-sm font-medium">
          {name}
        </h2>
        {budget?.over_threshold ? <Badge variant="warning">{t("quota.paused")}</Badge> : null}
      </header>

      <div className="space-y-2">
        <div className="flex items-center justify-between gap-2 text-xs text-muted-foreground">
          <span>{t("quota.reported")}</span>
          {quota ? <span title={quota.seen_at}>{t("quota.seen", { time: formatRelative(quota.seen_at, now) })}</span> : null}
        </div>
        {quota ? (
          quota.windows.map((w) => <WindowBar key={w.window} w={w} now={now} />)
        ) : (
          <p className="text-xs text-muted-foreground">{t("quota.no_report")}</p>
        )}
      </div>

      <div className="space-y-2 border-t pt-3">
        <p className="text-xs text-muted-foreground">{t("quota.counted")}</p>
        {budget ? (
          <Meter
            label={t("budget.hours", { hours: budget.window_hours })}
            pct={budget.pct}
            detail={t("quota.tokens_of", { used: fmt.number(budget.tokens_used), budget: fmt.number(budget.token_budget) })}
            right={budget.reset_at ? t("quota.resets", { time: formatRelative(budget.reset_at, now) }) : t("quota.threshold", { pct: budget.threshold_pct })}
            warn={budget.over_threshold}
          />
        ) : (
          <p className="text-xs text-muted-foreground">{t("quota.no_budget")}</p>
        )}
      </div>
    </section>
  )
}

function WindowBar({ w, now }: { w: QuotaWindow; now: number }) {
  const pct = Math.round(w.utilization * 100)
  const known = w.window === "five_hour" || w.window === "seven_day"
  return (
    <Meter
      label={known ? t(`quota.window.${w.window}` as MessageKey) : w.window}
      pct={pct}
      right={w.resets_at > 0 ? t("quota.resets", { time: formatRelative(new Date(w.resets_at * 1000).toISOString(), now) }) : ""}
      warn={pct >= 80}
    />
  )
}

function Meter({ label, pct, detail, right, warn }: { label: string; pct: number; detail?: string; right: string; warn: boolean }) {
  const clamped = Math.max(0, Math.min(100, pct))
  return (
    <div className="space-y-1">
      <div className="flex items-baseline justify-between gap-2">
        <span>{label}</span>
        <span className="text-xs text-muted-foreground">{right}</span>
      </div>
      <div className="h-1.5 overflow-hidden rounded-full bg-secondary" role="progressbar" aria-label={label} aria-valuemin={0} aria-valuemax={100} aria-valuenow={clamped}>
        <div className={cn("h-full rounded-full", warn ? "bg-status-blocked" : "bg-brand")} style={{ width: `${clamped}%` }} />
      </div>
      <div className="flex justify-between text-xs text-muted-foreground tabular-nums">
        <span>{detail ?? ""}</span>
        <span>{pct}%</span>
      </div>
    </div>
  )
}
