import { useState, type FormEvent } from "react"
import { Link } from "react-router-dom"
import { useQueryClient } from "@tanstack/react-query"
import { AlertTriangle, Plus } from "lucide-react"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Skeleton } from "@/components/ui/skeleton"
import { useKnowledge, type Lesson } from "@/hooks/useSystem"
import { addLesson, editLesson, removeLesson } from "@/lib/api"
import { taskPath } from "@/lib/nav"
import { t } from "@/i18n"

const field = "flex flex-col gap-1 text-xs text-muted-foreground"
const select = "h-7 rounded-md border bg-card px-2 text-xs text-foreground max-md:h-10"
const list = (s: string) => s.split(",").map((x) => x.trim()).filter(Boolean)

/**
 * System › Knowledge (#453): the project's lessons (.orch/knowledge/), how
 * often a prompt carried each and how those tasks did, so a useless lesson
 * shows. Search is the server's (orch_knowledge_search's ranking); area and
 * source filter here. Add, edit and delete go through the same editor as
 * `orch knowledge add|rm`.
 */
export function KnowledgePage() {
  const [q, setQ] = useState("")
  const [area, setArea] = useState("")
  const [source, setSource] = useState("")
  const [editing, setEditing] = useState<Lesson | "new" | null>(null)
  const [open, setOpen] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const queryClient = useQueryClient()
  const { data, isLoading, isError, error: readError } = useKnowledge(q.trim())
  const lessons = data?.lessons ?? []
  const areas = [...new Set(lessons.flatMap((l) => l.area ?? []))].sort()
  const shown = lessons.filter((l) => (!area || l.area?.includes(area)) && (!source || l.source === source))

  const remove = async (l: Lesson) => {
    if (!window.confirm(t("knowledge.confirm_delete", { id: l.id }))) return
    setError(null)
    try {
      await removeLesson(l.id)
      await queryClient.invalidateQueries({ queryKey: ["knowledge"] })
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }

  return (
    <div className="space-y-5 text-[13px]">
      <header className="flex flex-wrap items-center gap-2">
        <h1 className="sr-only">{t("knowledge.title")}</h1>
        <Input
          type="search"
          value={q}
          onChange={(e) => setQ(e.target.value)}
          placeholder={t("knowledge.search")}
          aria-label={t("knowledge.search")}
          className="h-7 w-64 max-md:h-10 max-md:w-full"
        />
        <select value={area} onChange={(e) => setArea(e.target.value)} aria-label={t("knowledge.col.area")} className={select}>
          <option value="">{t("knowledge.area_all")}</option>
          {areas.map((a) => (
            <option key={a} value={a}>
              {a}
            </option>
          ))}
        </select>
        <select value={source} onChange={(e) => setSource(e.target.value)} aria-label={t("knowledge.col.source")} className={select}>
          <option value="">{t("knowledge.source_all")}</option>
          {(data?.sources ?? []).map((s) => (
            <option key={s} value={s}>
              {s}
            </option>
          ))}
        </select>
        <div className="ml-auto flex gap-2">
          {/* ponytail: disabled until `orch knowledge curate` (#452) lands; then this runs it, estimate first. */}
          <span title={t("knowledge.curate_soon")}>
            <Button type="button" size="sm" variant="outline" disabled aria-describedby="curate-soon">
              {t("knowledge.curate")}
            </Button>
            <span id="curate-soon" className="sr-only">
              {t("knowledge.curate_soon")}
            </span>
          </span>
          <Button type="button" size="sm" onClick={() => setEditing("new")} className="gap-1">
            <Plus className="h-3.5 w-3.5" aria-hidden />
            {t("knowledge.add")}
          </Button>
        </div>
      </header>

      <p className="max-w-3xl leading-5 text-muted-foreground">{t("knowledge.intro")}</p>

      {isError || error ? (
        <Alert variant="destructive">
          <AlertTriangle className="h-4 w-4" />
          <AlertTitle>{t(error ? "knowledge.write_failed" : "knowledge.failed")}</AlertTitle>
          <AlertDescription>{error ?? readError?.message ?? t("common.unknown_error")}</AlertDescription>
        </Alert>
      ) : null}

      {isLoading ? (
        <Skeleton className="h-64 w-full" />
      ) : shown.length === 0 ? (
        <p className="text-muted-foreground">{t(lessons.length === 0 && !q.trim() ? "knowledge.none" : "knowledge.no_match")}</p>
      ) : (
        <div className="overflow-x-auto rounded-lg border">
          <table className="w-full min-w-[760px]">
            <thead className="border-b text-xs text-muted-foreground">
              <tr>
                <th className="px-3.5 py-2 text-left font-medium">{t("knowledge.col.lesson")}</th>
                <th className="px-3.5 py-2 text-left font-medium">{t("knowledge.col.area")}</th>
                <th className="px-3.5 py-2 text-left font-medium">{t("knowledge.col.source")}</th>
                <th className="px-3.5 py-2 text-left font-medium">{t("knowledge.col.task")}</th>
                <th className="px-3.5 py-2 text-left font-medium">{t("knowledge.col.date")}</th>
                <th className="px-3.5 py-2 text-right font-medium">{t("knowledge.col.prompts")}</th>
                <th className="px-3.5 py-2 text-right font-medium">{t("knowledge.col.first_try")}</th>
              </tr>
            </thead>
            <tbody>
              {shown.map((l) => (
                <LessonRow
                  key={l.id}
                  lesson={l}
                  open={open === l.id}
                  onToggle={() => setOpen(open === l.id ? null : l.id)}
                  onEdit={() => setEditing(l)}
                  onDelete={() => void remove(l)}
                />
              ))}
            </tbody>
          </table>
        </div>
      )}

      {editing ? (
        <LessonDialog
          lesson={editing === "new" ? null : editing}
          sources={data?.sources ?? []}
          onClose={() => setEditing(null)}
          onSaved={() => void queryClient.invalidateQueries({ queryKey: ["knowledge"] })}
        />
      ) : null}
    </div>
  )
}

function LessonRow({ lesson: l, open, onToggle, onEdit, onDelete }: { lesson: Lesson; open: boolean; onToggle: () => void; onEdit: () => void; onDelete: () => void }) {
  return (
    <>
      <tr className="border-b last:border-0 align-top">
        <td className="px-3.5 py-2">
          <button type="button" onClick={onToggle} aria-expanded={open} className="text-left font-medium hover:underline">
            {l.title}
          </button>
        </td>
        <td className="px-3.5 py-2 font-mono text-xs text-muted-foreground">{l.area?.join(", ") || "—"}</td>
        <td className="px-3.5 py-2">{l.source}</td>
        <td className="px-3.5 py-2 font-mono text-xs">
          {l.task ? (
            <Link to={taskPath(l.task)} className="hover:underline">
              {l.task}
            </Link>
          ) : (
            "—"
          )}
        </td>
        <td className="px-3.5 py-2 tabular-nums text-muted-foreground">{l.created}</td>
        <td className="px-3.5 py-2 text-right tabular-nums">{l.prompts}</td>
        <td className="px-3.5 py-2 text-right tabular-nums">{l.tasks > 0 ? t("knowledge.first_try", { first: l.first_try, tasks: l.tasks }) : "—"}</td>
      </tr>
      {open ? (
        <tr className="border-b last:border-0">
          <td colSpan={7} className="space-y-2 bg-muted/30 px-3.5 py-3">
            <p className="whitespace-pre-wrap leading-5">{l.body}</p>
            <p className="font-mono text-xs text-muted-foreground">{l.path}</p>
            <div className="flex gap-2">
              <Button type="button" size="sm" variant="outline" onClick={onEdit}>
                {t("knowledge.edit")}
              </Button>
              <Button type="button" size="sm" variant="outline" onClick={onDelete} className="text-status-failed">
                {t("knowledge.delete")}
              </Button>
            </div>
          </td>
        </tr>
      ) : null}
    </>
  )
}

function LessonDialog({ lesson, sources, onClose, onSaved }: { lesson: Lesson | null; sources: string[]; onClose: () => void; onSaved: () => void }) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const submit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    const form = new FormData(e.currentTarget)
    const text = (name: string) => String(form.get(name) ?? "").trim()
    const fields = {
      title: text("title"),
      body: text("body"),
      area: list(text("area")),
      tags: list(text("tags")),
      task: text("task") || undefined,
      source: text("source") || undefined,
    }
    setBusy(true)
    setError(null)
    try {
      await (lesson ? editLesson(lesson.id, fields) : addLesson(fields))
      onSaved()
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog open onOpenChange={(o) => (o ? null : onClose())}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>{t(lesson ? "knowledge.form.edit_title" : "knowledge.form.add_title")}</DialogTitle>
          <DialogDescription>{t("knowledge.form.intro")}</DialogDescription>
        </DialogHeader>
        <form onSubmit={(e) => void submit(e)} className="grid gap-3 text-[13px]">
          <label className={field}>
            {t("knowledge.form.title")}
            <Input name="title" required defaultValue={lesson?.title} autoFocus />
          </label>
          <label className={field}>
            {t("knowledge.form.body")}
            <textarea
              name="body"
              required
              rows={5}
              defaultValue={lesson?.body}
              className="rounded-md border border-input bg-background px-2.5 py-1.5 text-[13px] text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            />
          </label>
          <label className={field}>
            {t("knowledge.form.area")}
            <Input name="area" defaultValue={lesson?.area?.join(", ")} className="font-mono" />
          </label>
          <div className="grid grid-cols-3 gap-3">
            <label className={field}>
              {t("knowledge.form.tags")}
              <Input name="tags" defaultValue={lesson?.tags?.join(", ")} />
            </label>
            <label className={field}>
              {t("knowledge.col.task")}
              <Input name="task" defaultValue={lesson?.task} className="font-mono" />
            </label>
            <label className={field}>
              {t("knowledge.col.source")}
              <select name="source" defaultValue={lesson?.source ?? "operator"} className={`${select} h-9`}>
                {sources.map((s) => (
                  <option key={s} value={s}>
                    {s}
                  </option>
                ))}
              </select>
            </label>
          </div>
          {error ? (
            <p role="alert" className="text-xs text-status-failed">
              {error}
            </p>
          ) : null}
          <div className="flex justify-end gap-2">
            <Button type="button" variant="outline" size="sm" onClick={onClose}>
              {t("common.cancel")}
            </Button>
            <Button type="submit" size="sm" disabled={busy}>
              {t("knowledge.save")}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  )
}
