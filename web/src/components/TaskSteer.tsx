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
    <section className="space-y-2 border-t pt-4">
      <h3 className="text-xs font-medium text-muted-foreground">{t("steer.title")}</h3>
      <textarea
        aria-label={t("steer.placeholder")}
        className="min-h-16 w-full rounded-md border bg-background p-2 text-sm"
        placeholder={t("steer.placeholder")}
        value={note}
        maxLength={2000}
        onChange={(e) => setNote(e.target.value)}
      />
      <div className="flex flex-wrap gap-2">
        <Button
          size="sm"
          variant="outline"
          disabled={busy || note.trim() === ""}
          onClick={() => run(async () => (await noteTask(task.id, note), t("steer.noted")))}
        >
          {t("steer.note")}
        </Button>
        {running ? (
          <>
            <Button
              size="sm"
              variant="outline"
              disabled={busy}
              onClick={() => run(async () => (await stopTask(task.id, false, note)).message)}
            >
              {t("steer.redirect")}
            </Button>
            <Button
              size="sm"
              variant="destructive"
              disabled={busy}
              onClick={() => run(async () => (await stopTask(task.id, true, note)).message)}
            >
              {t("steer.stop_hold")}
            </Button>
          </>
        ) : null}
      </div>
      <p className="text-xs text-muted-foreground">{t("steer.hint")}</p>
      {result ? <p className="text-xs" role="status">{result}</p> : null}
      {error ? <p className="text-xs text-destructive" role="alert">{error}</p> : null}
    </section>
  )
}
