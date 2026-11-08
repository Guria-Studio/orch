import { useState } from "react"
import { useQueryClient } from "@tanstack/react-query"
import { AlertTriangle } from "lucide-react"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { useFindings, type Finding } from "@/hooks/useFindings"
import { fmt, t } from "@/i18n"
import { dismissFinding, sendFinding } from "@/lib/api"
import { cn } from "@/lib/utils"

/**
 * System › Findings (#536): the bugs this project's agents found in orch.
 * Nothing is public until the operator sends it: each card shows the exact
 * title and body that would be filed, secrets already redacted.
 */
export function FindingsPage() {
  const { data, isLoading, isError, error } = useFindings()
  const [showAll, setShowAll] = useState(false)
  const rows = (data?.findings ?? []).filter((f) => showAll || f.status === "queued")

  return (
    <div className="space-y-4 text-[13px]">
      <h1 className="sr-only">{t("findings.title")}</h1>
      <header className="flex flex-wrap items-center justify-between gap-3">
        <p className="max-w-prose text-muted-foreground">{t("findings.intro", { repo: data?.repo ?? "Guria-Studio/orch" })}</p>
        <label className="flex items-center gap-1.5 text-xs text-muted-foreground">
          <input type="checkbox" checked={showAll} onChange={(e) => setShowAll(e.target.checked)} />
          {t("findings.show_all")}
        </label>
      </header>

      {isError ? (
        <Alert variant="destructive">
          <AlertTriangle className="h-4 w-4" />
          <AlertTitle>{t("findings.load_failed")}</AlertTitle>
          <AlertDescription>{error?.message ?? t("common.unknown_error")}</AlertDescription>
        </Alert>
      ) : isLoading ? (
        <Skeleton className="h-40 w-full" />
      ) : rows.length === 0 ? (
        <p className="rounded-lg border border-dashed p-6 text-muted-foreground">{t("findings.none")}</p>
      ) : (
        <ul className="space-y-3">
          {rows.map((f) => (
            <FindingCard key={f.id} f={f} canSend={data?.can_send ?? false} />
          ))}
        </ul>
      )}
    </div>
  )
}

function FindingCard({ f, canSend }: { f: Finding; canSend: boolean }) {
  const queryClient = useQueryClient()
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState<string | null>(null)
  const act = async (fn: (id: number) => Promise<unknown>) => {
    setBusy(true)
    setErr(null)
    try {
      await fn(f.id)
      await queryClient.invalidateQueries({ queryKey: ["findings"] })
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }
  return (
    <li className={cn("rounded-lg border bg-card", f.status !== "queued" && "opacity-70")}>
      <header className="flex flex-wrap items-center gap-2 border-b px-3.5 py-2.5">
        <h2 className="min-w-0 flex-1 truncate font-semibold">{f.title}</h2>
        <span className="text-xs text-muted-foreground">
          #{f.id} · {fmt.dateTime(f.queued_at)}
          {f.task_id ? ` · ${f.task_id}` : ""}
        </span>
      </header>
      <pre aria-label={t("findings.preview")} className="max-h-80 overflow-auto whitespace-pre-wrap px-3.5 py-3 font-mono text-xs">
        {f.body}
      </pre>
      <footer className="flex flex-wrap items-center gap-2 border-t px-3.5 py-2.5">
        {f.status === "queued" ? (
          <>
            <Button size="sm" disabled={busy || !canSend} onClick={() => void act(sendFinding)}>
              {t("findings.send")}
            </Button>
            <Button size="sm" variant="outline" disabled={busy} onClick={() => void act(dismissFinding)}>
              {t("findings.dismiss")}
            </Button>
            {!canSend ? <span className="text-xs text-muted-foreground">{t("findings.cannot_send")}</span> : null}
          </>
        ) : f.status === "sent" ? (
          <a className="text-xs underline" href={f.url} target="_blank" rel="noreferrer">
            {t("findings.sent", { url: f.url })}
          </a>
        ) : (
          <span className="text-xs text-muted-foreground">{t("findings.dismissed")}</span>
        )}
        {err ? (
          <span role="alert" className="text-xs text-status-failed">
            {err}
          </span>
        ) : null}
      </footer>
    </li>
  )
}
