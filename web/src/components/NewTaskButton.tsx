import { useState, type FormEvent } from "react"
import { useQueryClient } from "@tanstack/react-query"
import { useNavigate } from "react-router-dom"
import { Plus } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { addTask } from "@/lib/api"
import { taskPath } from "@/lib/nav"
import { t } from "@/i18n"

const field = "flex flex-col gap-1 text-xs text-muted-foreground"

/**
 * "+ Task": adds a task through the same editor as `orch task add`
 * (POST /api/tasks, #421) and opens it. The server checks the id, the
 * dependencies and the model's route; a refusal shows its own words.
 */
export function NewTaskButton() {
  const [open, setOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const queryClient = useQueryClient()
  const navigate = useNavigate()

  const submit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    const form = new FormData(e.currentTarget)
    const text = (name: string) => String(form.get(name) ?? "").trim()
    const id = text("id")
    setBusy(true)
    setError(null)
    try {
      await addTask({
        id,
        title: text("title"),
        description: text("description") || undefined,
        model: text("model") || undefined,
        phase: text("phase") ? Number(text("phase")) : undefined,
        estimate_hours: text("estimate") ? Number(text("estimate")) : undefined,
        dependencies: text("deps") ? text("deps").split(/[\s,]+/).filter(Boolean) : undefined,
        backlog: form.get("backlog") === "on",
      })
      await queryClient.invalidateQueries({ queryKey: ["tasks"] })
      setOpen(false)
      navigate(taskPath(id))
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <Button type="button" size="sm" onClick={() => setOpen(true)} className="gap-1">
        <Plus className="h-3.5 w-3.5" aria-hidden />
        {t("newtask.button")}
      </Button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>{t("newtask.title")}</DialogTitle>
            <DialogDescription>{t("newtask.intro")}</DialogDescription>
          </DialogHeader>
          <form onSubmit={(e) => void submit(e)} className="grid gap-3 text-[13px]">
            <div className="grid grid-cols-[8rem_minmax(0,1fr)] gap-3">
              <label className={field}>
                {t("newtask.id")}
                <Input name="id" required placeholder="F2.T9" className="font-mono" autoFocus />
              </label>
              <label className={field}>
                {t("newtask.task_title")}
                <Input name="title" required />
              </label>
            </div>
            <div className="grid grid-cols-3 gap-3">
              <label className={field}>
                {t("tasks.col.phase")}
                <Input name="phase" type="number" min={0} inputMode="numeric" />
              </label>
              <label className={field}>
                {t("newtask.estimate")}
                <Input name="estimate" type="number" min={0} step="0.5" inputMode="decimal" />
              </label>
              <label className={field}>
                {t("tasks.col.model")}
                <Input name="model" placeholder="claude/sonnet" className="font-mono" />
              </label>
            </div>
            <label className={field}>
              {t("newtask.deps")}
              <Input name="deps" placeholder="F2.T5, F2.T6" className="font-mono" />
            </label>
            <label className={field}>
              {t("task.description")}
              <textarea name="description" rows={3} className="rounded-md border border-input bg-background px-2.5 py-1.5 text-[13px] text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring" />
            </label>
            <label className="flex items-center gap-2 text-xs">
              <input type="checkbox" name="backlog" className="h-4 w-4 accent-[var(--brand)]" />
              {t("newtask.backlog")}
            </label>
            {error ? (
              <p role="alert" className="text-xs text-status-failed">
                {error}
              </p>
            ) : null}
            <div className="flex justify-end gap-2">
              <Button type="button" variant="outline" size="sm" onClick={() => setOpen(false)}>
                {t("common.cancel")}
              </Button>
              <Button type="submit" size="sm" disabled={busy}>
                {t("newtask.create")}
              </Button>
            </div>
          </form>
        </DialogContent>
      </Dialog>
    </>
  )
}
