import { useState } from "react"
import { useQueryClient } from "@tanstack/react-query"
import { Button } from "@/components/ui/button"
import { useWhoami } from "@/hooks/useWhoami"
import { t } from "@/i18n"
import { noteTask, stopTask } from "@/lib/api"
import { statusKey } from "@/lib/status"
import type { Task } from "@/lib/types"

/**
 * The operator's controls on one task (#356): a note for its next attempt,
 * and — while it runs — stop and hold, or stop and redirect with the note.
 * The local operator only; the server refuses anyone else anyway.
 */
export function TaskSteer({ task }: { task: Task }) {
  const { data: whoami } = useWhoami()
  const queryClient = useQueryClient()
  const [note, setNote] = useState("")
  const [busy, setBusy] = useState(false)
  const [result, setResult] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  if (whoami?.profile !== "operator") return null
  const running = statusKey(task.status) === "in_progress"

  async function run(action: () => Promise<string>) {
    setBusy(true)
    setError(null)
    setResult(null)
    try {
      setResult(await action())
      setNote("")
      void queryClient.invalidateQueries({ queryKey: ["task", task.id] })
      void queryClient.invalidateQueries({ queryKey: ["tasks"] })
    } catch (err) {
      setError(t("steer.failed", { detail: err instanceof Error ? err.message : String(err) }))
    } finally {
      setBusy(false)
    }
  }

  return (
    <section aria-labelledby="steer-title" className="space-y-2 rounded-[10px] border bg-card p-3">
      <h2 id="steer-title" className="text-xs font-medium text-muted-foreground">
        {t("steer.title")}
      </h2>
      <textarea
        aria-label={t("steer.placeholder")}
        className="min-h-16 w-full resize-y rounded-md border bg-background px-2.5 py-2 text-[13px]"
        placeholder={t("steer.placeholder")}
        rows={3}
        value={note}
        maxLength={2000}
        onChange={(e) => setNote(e.target.value)}
      />
      <div className="flex flex-wrap items-center gap-2">
        <p className="text-xs text-muted-foreground">{t("steer.hint")}</p>
        <div className="ml-auto flex flex-wrap gap-1.5">
          {running ? (
            <>
              <Button
                size="sm"
                variant="outline"
                className="border-status-failed/40 text-status-failed hover:text-status-failed"
                disabled={busy}
                onClick={() => run(async () => (await stopTask(task.id, true, note)).message)}
              >
                {t("steer.stop_hold")}
              </Button>
              <Button size="sm" variant="outline" disabled={busy} onClick={() => run(async () => (await stopTask(task.id, false, note)).message)}>
                {t("steer.redirect")}
              </Button>
            </>
          ) : null}
          <Button size="sm" disabled={busy || note.trim() === ""} onClick={() => run(async () => (await noteTask(task.id, note), t("steer.noted")))}>
            {t("steer.note")}
          </Button>
        </div>
      </div>
      {result ? <p className="text-xs" role="status">{result}</p> : null}
      {error ? <p className="text-xs text-destructive" role="alert">{error}</p> : null}
    </section>
  )
}
