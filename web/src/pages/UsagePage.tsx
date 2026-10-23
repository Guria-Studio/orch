import { useEffect, useState } from "react"
import { useQueryClient } from "@tanstack/react-query"
import { Link, useSearchParams } from "react-router-dom"
import { AlertTriangle } from "lucide-react"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Skeleton } from "@/components/ui/skeleton"
import { useEventStream } from "@/hooks/useEventStream"
import { useMetrics, type MetricsBackendDay, type MetricsResponse, type UsagePeriod } from "@/hooks/useMetrics"
import { fmt, t, type MessageKey } from "@/i18n"
import { taskPath } from "@/lib/nav"
import { formatRelative } from "@/lib/time"
import { cn } from "@/lib/utils"

const PERIODS: UsagePeriod[] = ["today", "24h", "7d", "30d", "all"]
type Metric = "tokens" | "dispatches" | "cost"

// One color per backend, the same on every chart; anything else is gray.
const BACKEND_COLORS: Record<string, string> = {
  claude: "#d97757",
  codex: "#3fa38a",
  opencode: "#8b7bd8",
  gemini: "#4a8fe0",
  agy: "#d16ba5",
}
const backendColor = (backend: string) => BACKEND_COLORS[backend] ?? "#8a8f98"

/**
 * Usage: what the agents consumed, from orch's spend rows (one per dispatch)
 * — the totals by kind of token, the cost (marked when part of it is priced
 * from pricing.yaml rather than reported), a day-by-day chart stacked by
 * provider, the latest dispatches and the per-model table. Replaces Metrics;
 * the rolling window the budget gate enforces stays on Budget.
 */
export function UsagePage() {
  const [params, setParams] = useSearchParams()
  const requested = params.get("period") as UsagePeriod | null
  const period: UsagePeriod = requested && PERIODS.includes(requested) ? requested : "7d"
  const { data, isLoading, isError, error } = useMetrics(period)
  const queryClient = useQueryClient()
  const { lastEventAt } = useEventStream()

  // A run that finishes a dispatch adds a spend row: refresh with it.
  useEffect(() => {
    if (lastEventAt) void queryClient.invalidateQueries({ queryKey: ["metrics"] })
  }, [lastEventAt, queryClient])

  const choose = (next: UsagePeriod) => {
    const p = new URLSearchParams(params)
    p.set("period", next)
    setParams(p, { replace: true })
  }

  return (
    <div className="space-y-6 text-[13px]">
      <header className="flex flex-wrap items-start justify-between gap-4">
        <div className="space-y-1">
          <h1 className="text-xl font-semibold tracking-[-0.01em]">{t("usage.title")}</h1>
          <p className="text-muted-foreground">{t("usage.intro")}</p>
        </div>
        <div role="radiogroup" aria-label={t("usage.period")} className="inline-flex rounded-md border p-0.5">
          {PERIODS.map((p) => (
            <button
              key={p}
              type="button"
              role="radio"
              aria-checked={p === period}
              onClick={() => choose(p)}
              className={cn(
                "h-7 rounded px-2.5 text-xs transition-colors",
                p === period ? "bg-secondary font-medium text-foreground" : "text-muted-foreground hover:text-foreground",
              )}
            >
              {t(`usage.period.${p}` as MessageKey)}
            </button>
          ))}
        </div>
      </header>

      {isError ? (
        <Alert variant="destructive">
          <AlertTriangle className="h-4 w-4" />
          <AlertTitle>{t("metrics.load_failed")}</AlertTitle>
          <AlertDescription>{error?.message ?? t("common.unknown_error")}</AlertDescription>
        </Alert>
      ) : null}

      {isLoading || !data ? <Skeleton className="h-96 w-full" /> : <UsageBody data={data} />}
    </div>
  )
}

function UsageBody({ data }: { data: MetricsResponse }) {
  const estimated = data.estimated_cost_usd > 0
  const kpis: { label: MessageKey; value: string; tone?: string }[] = [
    { label: "usage.kpi.dispatches", value: fmt.number(data.dispatches) },
    { label: "usage.kpi.input", value: fmt.number(data.tokens_in), tone: "text-[#d97757]" },
    { label: "usage.kpi.cached", value: fmt.number(data.cache_read_tokens), tone: "text-[#4a8fe0]" },
    { label: "usage.kpi.output", value: fmt.number(data.tokens_out), tone: "text-[#3fa38a]" },
    { label: "usage.kpi.cost", value: `${estimated ? "~" : ""}${fmt.usd(data.total_cost_usd)}` },
  ]
  return (
    <>
      <section className="grid grid-cols-2 gap-3 md:grid-cols-5">
        {kpis.map((k) => (
          <div key={k.label} className="rounded-lg border bg-card px-3.5 py-3">
            <p className="text-xs text-muted-foreground">{t(k.label)}</p>
            <p className={cn("mt-1 text-xl font-semibold tabular-nums tracking-[-0.01em]", k.tone)}>{k.value}</p>
            {k.label === "usage.kpi.cost" && estimated ? <p className="mt-1 text-[11px] text-muted-foreground">{t("usage.cost_estimated")}</p> : null}
          </div>
        ))}
      </section>
      {data.estimate_hours_total > 0 ? <p className="text-xs text-muted-foreground">{t("usage.hours", { hours: data.estimate_hours_total })}</p> : null}

      <div className="grid gap-4 xl:grid-cols-[minmax(0,3fr)_minmax(0,2fr)]">
        <DayChart rows={data.by_backend_day} />
        <Recent data={data} />
      </div>

      <section aria-labelledby="usage-models" className="space-y-2">
        <h2 id="usage-models" className="text-xs font-medium text-muted-foreground">
          {t("metrics.table")}
        </h2>
        <div className="overflow-x-auto rounded-lg border">
          <table className="w-full min-w-[560px]">
            <thead className="border-b text-xs text-muted-foreground">
              <tr>
                <th className="px-3.5 py-2 text-left font-medium">{t("metrics.col.model")}</th>
                <th className="px-3.5 py-2 text-right font-medium">{t("metrics.col.tasks")}</th>
                <th className="px-3.5 py-2 text-right font-medium">{t("metrics.col.tokens_in")}</th>
                <th className="px-3.5 py-2 text-right font-medium">{t("metrics.col.tokens_out")}</th>
                <th className="px-3.5 py-2 text-right font-medium">{t("metrics.col.cost")}</th>
              </tr>
            </thead>
            <tbody className="divide-y">
              {data.by_model.length === 0 ? (
                <tr>
                  <td colSpan={5} className="px-3.5 py-6 text-center text-muted-foreground">
                    {t("usage.none")}
                  </td>
                </tr>
              ) : (
                data.by_model.map((row) => (
                  <tr key={row.model}>
                    <td className="px-3.5 py-2 font-mono text-xs">{row.model}</td>
                    <td className="px-3.5 py-2 text-right tabular-nums">{row.tasks_total}</td>
                    <td className="px-3.5 py-2 text-right tabular-nums">{fmt.number(row.tokens_in)}</td>
                    <td className="px-3.5 py-2 text-right tabular-nums">{fmt.number(row.tokens_out)}</td>
                    <td className="px-3.5 py-2 text-right tabular-nums">{fmt.usd(row.cost_usd)}</td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </section>
    </>
  )
}

/** Days oldest → newest, each a column stacked by backend. */
function DayChart({ rows }: { rows: MetricsBackendDay[] }) {
  const [metric, setMetric] = useState<Metric>("tokens")
  const value = (r: MetricsBackendDay) => (metric === "tokens" ? r.tokens : metric === "dispatches" ? r.dispatches : r.cost_usd)
  const days = [...new Set(rows.map((r) => r.date))].sort()
  const backends = [...new Set(rows.map((r) => r.backend))].sort()
  const totals = days.map((d) => rows.filter((r) => r.date === d).reduce((acc, r) => acc + value(r), 0))
  const max = Math.max(0, ...totals)
  const show = (n: number) => (metric === "cost" ? fmt.usd(n) : fmt.number(n))

  return (
    <section aria-labelledby="usage-chart" className="space-y-3 rounded-lg border bg-card p-3.5">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h2 id="usage-chart" className="text-xs font-medium text-muted-foreground">
          {t("usage.chart")}
        </h2>
        <div role="tablist" aria-label={t("usage.chart")} className="flex gap-1">
          {(["tokens", "dispatches", "cost"] as const).map((m) => (
            <button
              key={m}
              type="button"
              role="tab"
              aria-selected={metric === m}
              onClick={() => setMetric(m)}
              className={cn("h-6 rounded px-2 text-xs", metric === m ? "bg-secondary font-medium" : "text-muted-foreground hover:text-foreground")}
            >
              {t(`usage.metric.${m}` as MessageKey)}
            </button>
          ))}
        </div>
      </div>
      {days.length === 0 || max === 0 ? (
        <p className="py-10 text-center text-muted-foreground">{t("usage.none")}</p>
      ) : (
        <>
          <div className="flex h-40 items-end gap-1.5" role="img" aria-label={days.map((d, i) => `${fmt.day(d)}: ${show(totals[i])}`).join(", ")}>
            {days.map((d, i) => (
              <div key={d} className="flex h-full min-w-0 flex-1 flex-col items-stretch justify-end" title={`${fmt.day(d)} · ${show(totals[i])}`}>
                {backends.map((b) => {
                  const v = rows.find((r) => r.date === d && r.backend === b)
                  const n = v ? value(v) : 0
                  return n > 0 ? <div key={b} style={{ height: `${(n / max) * 100}%`, backgroundColor: backendColor(b) }} className="first:rounded-t-sm" /> : null
                })}
              </div>
            ))}
          </div>
          <div className="flex gap-1.5 text-[10px] text-muted-foreground" aria-hidden>
            {days.map((d) => (
              <span key={d} className="min-w-0 flex-1 truncate text-center">
                {fmt.day(d)}
              </span>
            ))}
          </div>
          <ul className="flex flex-wrap gap-3 text-xs">
            {backends.map((b) => (
              <li key={b} className="inline-flex items-center gap-1.5">
                <span className="h-2 w-2 rounded-sm" style={{ backgroundColor: backendColor(b) }} aria-hidden />
                {b}
              </li>
            ))}
          </ul>
        </>
      )}
    </section>
  )
}

function Recent({ data }: { data: MetricsResponse }) {
  // Read once per mount: the relative times need not tick.
  const [now] = useState(() => Date.now())
  return (
    <section aria-labelledby="usage-recent" className="space-y-2 rounded-lg border bg-card p-3.5">
      <h2 id="usage-recent" className="text-xs font-medium text-muted-foreground">
        {t("usage.recent")}
      </h2>
      {data.recent.length === 0 ? (
        <p className="py-6 text-center text-muted-foreground">{t("usage.none")}</p>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full min-w-[420px] text-xs">
            <thead className="text-muted-foreground">
              <tr>
                <th className="py-1.5 pr-2 text-left font-medium">{t("usage.col.task")}</th>
                <th className="py-1.5 pr-2 text-left font-medium">{t("usage.col.backend")}</th>
                <th className="py-1.5 pr-2 text-right font-medium">{t("usage.col.in_out")}</th>
                <th className="py-1.5 text-right font-medium">{t("usage.col.when")}</th>
              </tr>
            </thead>
            <tbody className="divide-y">
              {data.recent.map((r, i) => (
                <tr key={`${r.ts}-${r.task_id}-${i}`}>
                  <td className="py-1.5 pr-2 font-mono">
                    {r.task_id ? (
                      <Link to={taskPath(r.task_id)} className="hover:underline">
                        {r.task_id}
                      </Link>
                    ) : (
                      "—"
                    )}
                  </td>
                  <td className="max-w-[12rem] truncate py-1.5 pr-2">
                    <span className="mr-1.5 inline-block h-1.5 w-1.5 rounded-full align-middle" style={{ backgroundColor: backendColor(r.backend) }} aria-hidden />
                    {r.model || r.backend}
                    {r.estimated ? <span className="ml-1.5 text-muted-foreground">· {t("usage.estimated")}</span> : null}
                  </td>
                  <td className="whitespace-nowrap py-1.5 pr-2 text-right tabular-nums">
                    {fmt.number(r.tokens_in)}
                    <span className="text-[#d97757]" aria-hidden>↑</span> {fmt.number(r.tokens_out)}
                    <span className="text-[#3fa38a]" aria-hidden>↓</span>
                  </td>
                  <td className="whitespace-nowrap py-1.5 text-right text-muted-foreground" title={r.ts}>
                    {formatRelative(r.ts, now)}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  )
}
