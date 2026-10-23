import { useState } from "react"
import { useQueryClient } from "@tanstack/react-query"
import { AlertTriangle } from "lucide-react"
import { BudgetLimitsButton } from "@/components/BudgetLimitsDialog"
import { RoutingTable } from "@/components/RoutingTable"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Skeleton } from "@/components/ui/skeleton"
import { useBudgetSummary, type BudgetRow } from "@/hooks/useBudgetSummary"
import { useQuota, type ProviderQuota, type QuotaWindow } from "@/hooks/useQuota"
import { t, type MessageKey } from "@/i18n"
import { setProvider } from "@/lib/api"
import { formatRelative } from "@/lib/time"
import { cn } from "@/lib/utils"

type Source = "reported" | "estimated" | "none"

const SOURCE_TONE: Record<Source, string> = {
  reported: "bg-status-done/15 text-status-done",
  estimated: "bg-brand/15 text-brand",
  none: "bg-muted text-muted-foreground",
}

/**
 * System › Providers and quota: one card per provider with how close it is
 * to a limit — the subscription windows the provider itself reported
 * (claude's 5-hour and weekly, from its latest dispatch; "reported"), or
 * orch's own budget window from budgets.yaml ("estimated"), which pauses
 * dispatches at its threshold.
 */
// The product each backend's CLI is, as the cards name it: orch runs the CLI
// under the operator's own login, so the quota is that session's.
const PRODUCT: Record<string, string> = {
  claude: "Claude Code",
  codex: "OpenAI Codex",
  opencode: "opencode",
  gemini: "Gemini CLI",
  agy: "Antigravity",
  muse: "Muse Code",
}

export function QuotaPage() {
  const quota = useQuota()
  const budget = useBudgetSummary()
  const loading = quota.isLoading || budget.isLoading
  const reported = quota.data?.providers ?? []
  const rows: BudgetRow[] = budget.data?.available ? budget.data.rows : []
  const names = [...new Set([...reported.map((q) => q.backend), ...rows.map((r) => r.provider)])].sort()

  return (
    <div className="space-y-5 text-[13px]">
      <header className="flex flex-wrap items-center justify-between gap-3">
        <h1 className="sr-only">{t("quota.title")}</h1>
        <div className="flex flex-wrap gap-4 text-xs text-muted-foreground">
          {(["reported", "estimated"] as const).map((s) => (
            <span key={s} className="inline-flex items-center gap-1.5">
              <SourceChip source={s} />
              {t(`quota.legend.${s}` as MessageKey)}
            </span>
          ))}
        </div>
        {budget.data?.available ? (
          <span className="flex items-center gap-3">
            <span className="text-xs text-muted-foreground">{t("quota.preset", { preset: budget.data.preset })}</span>
            <BudgetLimitsButton preset={budget.data.preset} rows={rows} />
          </span>
        ) : null}
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
        <section aria-label={t("quota.title")} className="grid gap-3 lg:grid-cols-2">
          {names.map((name) => (
            <ProviderCard key={name} name={name} quota={reported.find((q) => q.backend === name)} budget={rows.find((r) => r.provider === name)} />
          ))}
        </section>
      )}

      <RoutingTable />
    </div>
  )
}

function SourceChip({ source }: { source: Source }) {
  return (
    <span className={cn("rounded px-1.5 py-0.5 text-[11px] font-medium", SOURCE_TONE[source])}>{t(`quota.source.${source}` as MessageKey)}</span>
  )
}

/**
 * The design's on/off switch: `orch provider disable|enable` through the
 * dashboard. Off, `orch run` leaves the provider's tasks waiting.
 */
function ProviderSwitch({ name, off, onError }: { name: string; off: boolean; onError: (message: string | null) => void }) {
  const queryClient = useQueryClient()
  const [busy, setBusy] = useState(false)
  const flip = async () => {
    setBusy(true)
    onError(null)
    try {
      await setProvider(name, !off)
      await queryClient.invalidateQueries({ queryKey: ["quota"] })
    } catch (err) {
      onError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }
  return (
    <button
      type="button"
      role="switch"
      aria-checked={!off}
      aria-label={t("quota.switch", { provider: name })}
      disabled={busy}
      onClick={() => void flip()}
      className={cn(
        "relative h-5 w-[34px] shrink-0 rounded-full transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-60",
        off ? "bg-secondary" : "bg-brand",
      )}
    >
      <span className={cn("absolute top-[3px] h-3.5 w-3.5 rounded-full bg-white transition-[left]", off ? "left-[3px]" : "left-[17px]")} />
    </button>
  )
}

function ProviderCard({ name, quota, budget }: { name: string; quota?: ProviderQuota; budget?: BudgetRow }) {
  // Read once per mount: the relative times need not tick.
  const [now] = useState(() => Date.now())
  const [error, setError] = useState<string | null>(null)
  const off = quota?.disabled === true
  // A provider listed only because it is turned off carries no report.
  const report = quota && quota.windows.length > 0 ? quota : undefined
  const source: Source = report ? "reported" : budget ? "estimated" : "none"
  const note = report
    ? t("quota.note.reported", { time: formatRelative(report.seen_at, now) })
    : budget
      ? t("quota.note.estimated", { pct: budget.threshold_pct })
      : t("quota.note.none")
  return (
    <article aria-labelledby={`quota-${name}`} className={cn("rounded-lg border bg-card", off && "opacity-80")}>
      <header className="flex items-center gap-2 border-b px-3.5 py-2.5">
        <span className="flex min-w-0 flex-col">
          <h2 id={`quota-${name}`} className="font-semibold">
            {name}
          </h2>
          {PRODUCT[name] ? <span className="text-xs text-muted-foreground">{t("quota.cli_session", { product: PRODUCT[name] })}</span> : null}
        </span>
        <span className="ml-auto">
          <SourceChip source={source} />
        </span>
        <ProviderSwitch name={name} off={off} onError={setError} />
      </header>
      <div className="space-y-2.5 px-3.5 py-3">
        {off ? (
          <p className="text-xs text-status-blocked">{quota?.reason ? t("quota.off_reason", { reason: quota.reason }) : t("quota.off")}</p>
        ) : null}
        {error ? (
          <p role="alert" className="text-xs text-status-failed">
            {error}
          </p>
        ) : null}
        {report ? report.windows.map((w) => <ReportedRow key={w.window} w={w} now={now} />) : null}
        {!report && budget ? (
          <WindowRow
            label={t("quota.window.budget", { hours: budget.window_hours })}
            pct={budget.pct}
            reset={budget.reset_at ? formatRelative(budget.reset_at, now) : ""}
            warn={budget.over_threshold || budget.pct >= 80}
          />
        ) : null}
        <p className="text-xs text-muted-foreground">{note}</p>
      </div>
    </article>
  )
}

function ReportedRow({ w, now }: { w: QuotaWindow; now: number }) {
  const known = ["five_hour", "seven_day", "session"].includes(w.window)
  const pct = Math.round(w.utilization * 100)
  return (
    <WindowRow
      label={known ? t(`quota.window.${w.window}` as MessageKey) : w.window}
      pct={pct}
      reset={w.resets_at > 0 ? formatRelative(new Date(w.resets_at * 1000).toISOString(), now) : ""}
      warn={pct >= 80}
    />
  )
}

/** One window: label, bar, percentage and when it resets, on one line. */
function WindowRow({ label, pct, reset, warn }: { label: string; pct: number; reset: string; warn: boolean }) {
  const clamped = Math.max(0, Math.min(100, pct))
  return (
    <div className="grid grid-cols-[7.5rem_minmax(0,1fr)_3rem_6.5rem] items-center gap-3 max-sm:grid-cols-[6rem_minmax(0,1fr)_3rem]">
      <span className="truncate">{label}</span>
      <span className="h-1.5 overflow-hidden rounded-full bg-secondary" role="progressbar" aria-label={label} aria-valuemin={0} aria-valuemax={100} aria-valuenow={clamped}>
        <span className={cn("block h-full rounded-full", warn ? "bg-status-blocked" : "bg-brand")} style={{ width: `${clamped}%` }} />
      </span>
      <span className="text-right tabular-nums">{pct}%</span>
      <span className="truncate text-right text-xs text-muted-foreground max-sm:hidden">{reset}</span>
    </div>
  )
}
