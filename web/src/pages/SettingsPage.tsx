import { AlertTriangle } from "lucide-react"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Skeleton } from "@/components/ui/skeleton"
import { useEffectiveConfig } from "@/hooks/useSystem"
import { t, type MessageKey } from "@/i18n"
import { cn } from "@/lib/utils"

// The design's sections and the config keys each shows, in its order.
const SECTIONS: { id: string; keys: string[] }[] = [
  { id: "proyecto", keys: ["spec_root", "dashboard.language"] },
  { id: "ejecucion", keys: ["concurrency.global_max", "dispatch.worktree_mode", "dispatch.base_branch", "dispatch.worktree_setup", "dispatch.tdd"] },
  { id: "reintentos", keys: ["retry.max_attempts", "retry.backoff_seconds", "retry.rate_limit_backoff_seconds"] },
  { id: "verify", keys: ["verify.enabled", "verify.review", "verify.reviewer", "verify.app.start"] },
  { id: "hallazgos", keys: ["report_findings.enabled"] },
  { id: "presupuesto", keys: ["budget.per_dispatch_usd", "budgets_preset"] },
  { id: "notificaciones", keys: ["notifications.slack_webhook", "notifications.budget_alerts", "notifications.budget_alert_pct"] },
  { id: "tunel", keys: ["tunnel.enabled", "tunnel.url_parse_timeout_s"] },
  { id: "telemetria", keys: ["telemetry.enabled"] },
]

/**
 * System › Settings: the effective configuration — config.yaml merged with
 * the defaults, as `orch config show` computes it — in the design's
 * sections, each value under the key docs/CONFIG.md documents. Read-only:
 * orch has no command that writes config.yaml, so the switches and fields
 * show the values and the file stays the interface. Credentials only say
 * whether they are set.
 */
export function SettingsPage() {
  const { data, isLoading, isError, error } = useEffectiveConfig()
  const values = new Map<string, string>()
  for (const sec of data?.sections ?? []) for (const e of sec.entries) values.set(e.key, e.value)

  return (
    <div className="space-y-6 text-[13px]">
      <header className="flex flex-wrap items-center gap-3">
        <h1 className="sr-only">{t("settings.title")}</h1>
        {data ? <code className="rounded-md bg-secondary px-2 py-0.5 font-mono text-xs">{data.path}</code> : null}
        <span className="ml-auto text-xs text-muted-foreground">{t("settings.readonly")}</span>
      </header>

      {isError || data?.error ? (
        <Alert variant="destructive">
          <AlertTriangle className="h-4 w-4" />
          <AlertTitle>{t("settings.failed")}</AlertTitle>
          <AlertDescription className="font-mono text-xs">{data?.error ?? error?.message ?? t("common.unknown_error")}</AlertDescription>
        </Alert>
      ) : null}

      {isLoading ? (
        <Skeleton className="h-96 w-full" />
      ) : data && !data.error ? (
        <div className="grid gap-8 lg:grid-cols-[11rem_minmax(0,1fr)]">
          <nav aria-label={t("settings.sections")} className="lg:sticky lg:top-16 lg:self-start">
            <ul className="flex flex-wrap gap-1 lg:flex-col">
              {SECTIONS.map((sec) => (
                <li key={sec.id}>
                  <a href={`#${sec.id}`} className="block rounded-md px-2.5 py-1 text-muted-foreground hover:bg-secondary hover:text-foreground">
                    {t(`settings.sec.${sec.id}` as MessageKey)}
                  </a>
                </li>
              ))}
            </ul>
          </nav>

          <div className="min-w-0 space-y-8">
            {SECTIONS.map((sec) => (
              <section key={sec.id} id={sec.id} aria-labelledby={`${sec.id}-h`} className="scroll-mt-20 space-y-3">
                <div className="space-y-0.5">
                  <h2 id={`${sec.id}-h`} className="text-sm font-medium">
                    {t(`settings.sec.${sec.id}` as MessageKey)}
                  </h2>
                  <p className="text-xs text-muted-foreground">{t(`settings.sec.${sec.id}.desc` as MessageKey)}</p>
                </div>
                <div className="divide-y rounded-lg border bg-card">
                  {sec.keys
                    .filter((key) => values.has(key))
                    .map((key) => (
                      <Field key={key} configKey={key} value={values.get(key) ?? ""} />
                    ))}
                </div>
              </section>
            ))}
          </div>
        </div>
      ) : null}
    </div>
  )
}

function Field({ configKey, value }: { configKey: string; value: string }) {
  const label = t(`settings.f.${configKey}` as MessageKey)
  const isBool = value === "true" || value === "false"
  const shown = value === "" || value === '""' ? "—" : value
  return (
    <div className="flex flex-wrap items-center gap-x-4 gap-y-2 px-3.5 py-2.5">
      <span className="min-w-0 flex-1">
        <span className="block">{label}</span>
        <code className="font-mono text-[11px] text-muted-foreground">{configKey}</code>
      </span>
      {isBool ? (
        <button
          type="button"
          role="switch"
          aria-checked={value === "true"}
          aria-label={label}
          disabled
          className={cn("relative h-5 w-[34px] shrink-0 rounded-full disabled:cursor-default", value === "true" ? "bg-brand" : "bg-secondary")}
        >
          <span className={cn("absolute top-[3px] h-3.5 w-3.5 rounded-full bg-white", value === "true" ? "left-[17px]" : "left-[3px]")} />
        </button>
      ) : (
        <input
          aria-label={label}
          value={shown}
          readOnly
          className="h-8 w-full max-w-xs rounded-md border bg-background px-2.5 font-mono text-xs text-foreground sm:w-64"
        />
      )}
    </div>
  )
}
