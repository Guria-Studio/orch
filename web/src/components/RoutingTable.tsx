import { useRouterTable } from "@/hooks/useBudgetLimits"
import { t } from "@/i18n"

/** model_router.yaml as the Quota page shows it: which provider and model each task `model` runs on, and how many at once. */
export function RoutingTable() {
  const { data } = useRouterTable()
  if (!data) return null
  return (
    <section aria-labelledby="router-h" className="space-y-2">
      <div className="flex flex-wrap items-center gap-2.5">
        <h2 id="router-h" className="text-xs font-medium text-muted-foreground">
          {t("router.title")}
        </h2>
        <code className="font-mono text-xs text-muted-foreground">model_router.yaml</code>
        <span className="ml-auto text-xs text-muted-foreground">
          concurrency.global_max <span className="font-medium text-foreground tabular-nums">{data.global_max}</span>
        </span>
      </div>
      {data.routes.length === 0 ? (
        <p className="rounded-lg border border-dashed p-4 text-xs text-muted-foreground">{data.error ?? t("router.none")}</p>
      ) : (
        <div className="overflow-x-auto rounded-lg border">
          <table className="w-full min-w-[520px] text-[13px]">
            <thead className="border-b text-xs text-muted-foreground">
              <tr>
                <th className="px-3.5 py-2 text-left font-medium">{t("router.when")}</th>
                <th className="px-3.5 py-2 text-left font-medium">{t("router.provider")}</th>
                <th className="px-3.5 py-2 text-left font-medium">{t("router.model")}</th>
                <th className="px-3.5 py-2 text-right font-medium">{t("router.parallel")}</th>
              </tr>
            </thead>
            <tbody className="divide-y">
              {data.routes.map((r) => (
                <tr key={r.key}>
                  <td className="px-3.5 py-2 font-mono text-xs">{r.key}</td>
                  <td className="px-3.5 py-2">{r.backend}</td>
                  <td className="px-3.5 py-2 font-mono text-xs text-muted-foreground">{r.cli_model}</td>
                  <td className="px-3.5 py-2 text-right tabular-nums">{r.max_parallel}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  )
}
