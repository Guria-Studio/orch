import { AlertTriangle } from "lucide-react"
import { CopyCommand } from "@/components/CopyCommand"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Skeleton } from "@/components/ui/skeleton"
import { useEffectiveConfig } from "@/hooks/useSystem"
import { t } from "@/i18n"

/**
 * System › Settings: the effective configuration — config.yaml merged with
 * the defaults, as `orch config show` prints it — by section, each value
 * under the dotted key docs/CONFIG.md documents. Read-only: orch has no
 * command that writes config.yaml, so the file stays the interface.
 * Credentials (the stakeholder token, webhooks) only say whether they are set.
 */
export function SettingsPage() {
  const { data, isLoading, isError, error } = useEffectiveConfig()

  return (
    <div className="max-w-5xl space-y-6 text-[13px]">
      <header className="space-y-1">
        <h1 className="text-xl font-semibold tracking-[-0.01em]">{t("settings.title")}</h1>
        <p className="max-w-3xl text-muted-foreground">{t("settings.intro")}</p>
      </header>

      {isError || data?.error ? (
        <Alert variant="destructive">
          <AlertTriangle className="h-4 w-4" />
          <AlertTitle>{t("settings.failed")}</AlertTitle>
          <AlertDescription className="font-mono text-xs">{data?.error ?? error?.message ?? t("common.unknown_error")}</AlertDescription>
        </Alert>
      ) : null}

      {isLoading || !data ? (
        isLoading ? <Skeleton className="h-96 w-full" /> : null
      ) : (
        <div className="grid gap-6 lg:grid-cols-[11rem_minmax(0,1fr)]">
          <nav aria-label={t("settings.sections")} className="lg:sticky lg:top-16 lg:self-start">
            <ul className="flex flex-wrap gap-1 lg:flex-col">
              {data.sections.map((sec) => (
                <li key={sec.name}>
                  <a href={`#settings-${sec.name}`} className="block rounded-md px-2.5 py-1 font-mono text-xs text-muted-foreground hover:bg-secondary hover:text-foreground">
                    {sec.name}
                  </a>
                </li>
              ))}
            </ul>
          </nav>

          <div className="min-w-0 space-y-6">
            <section className="space-y-1 rounded-lg border bg-card px-3.5 py-3 text-xs">
              <p className="text-muted-foreground">{t("settings.sources")}</p>
              {data.sources.length > 0 ? (
                data.sources.map((src) => (
                  <p key={src} className="font-mono">
                    {src}
                  </p>
                ))
              ) : (
                <p>{t("settings.defaults_only")}</p>
              )}
              {data.warnings.length > 0 ? (
                <div className="pt-2">
                  <p className="text-status-blocked">{t("settings.ignored")}</p>
                  {data.warnings.map((w) => (
                    <p key={w} className="font-mono">
                      {w}
                    </p>
                  ))}
                </div>
              ) : null}
            </section>

            {data.sections.map((sec) => (
              <section key={sec.name} id={`settings-${sec.name}`} aria-labelledby={`settings-h-${sec.name}`} className="scroll-mt-20 space-y-2">
                <h2 id={`settings-h-${sec.name}`} className="font-mono text-xs font-medium text-muted-foreground">
                  {sec.name}
                </h2>
                <dl className="divide-y rounded-lg border">
                  {sec.entries.map((e) => (
                    <div key={e.key} className="grid gap-x-4 gap-y-0.5 px-3.5 py-2 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
                      <dt className="truncate font-mono text-xs" title={e.key}>
                        {e.key}
                      </dt>
                      <dd className="break-all font-mono text-xs text-muted-foreground">{e.value || "—"}</dd>
                    </div>
                  ))}
                </dl>
              </section>
            ))}

            <section className="space-y-1.5">
              <p className="text-xs font-medium">{t("settings.show")}</p>
              <CopyCommand command="orch config show" className="mt-0" />
            </section>
          </div>
        </div>
      )}
    </div>
  )
}
