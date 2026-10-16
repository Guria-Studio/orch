import { useLayoutEffect, useRef } from "react"
import { AlertTriangle, CheckCircle2, MessageSquare, Play, Wrench } from "lucide-react"
import { useTaskActivity, type ActivityStep } from "@/hooks/useTaskActivity"
import { t } from "@/i18n"
import { cn } from "@/lib/utils"

// gemini and agy print one object when they finish: nothing to follow before.
const NOT_STREAMED = new Set(["gemini", "agy"])

function StepRow({ step }: { step: ActivityStep }) {
  switch (step.kind) {
    case "tool":
      return (
        <li className="flex items-baseline gap-2">
          <Wrench className="h-3 w-3 shrink-0 translate-y-0.5 text-status-running" aria-hidden />
          <span className="shrink-0 font-medium text-foreground">{step.tool}</span>
          <span className="min-w-0 truncate text-muted-foreground" title={step.detail}>
            {step.detail}
          </span>
        </li>
      )
    case "text":
      return (
        <li className="flex items-baseline gap-2">
          <MessageSquare className="h-3 w-3 shrink-0 translate-y-0.5 text-muted-foreground" aria-hidden />
          <span className="min-w-0 whitespace-pre-wrap break-words font-sans text-foreground/90">{step.detail}</span>
        </li>
      )
    case "error":
      return (
        <li className="flex items-baseline gap-2 text-status-failed">
          <AlertTriangle className="h-3 w-3 shrink-0 translate-y-0.5" aria-hidden />
          <span className="min-w-0 truncate" title={step.detail}>
            {step.detail}
          </span>
        </li>
      )
    case "start":
      return (
        <li className="flex items-baseline gap-2 text-muted-foreground">
          <Play className="h-3 w-3 shrink-0 translate-y-0.5" aria-hidden />
          {t("activity.started")}
        </li>
      )
    case "end":
      return (
        <li className="flex items-baseline gap-2 text-status-done">
          <CheckCircle2 className="h-3 w-3 shrink-0 translate-y-0.5" aria-hidden />
          {t("activity.done")}
        </li>
      )
  }
}

/** What a task's agent is doing, step by step; followed live while `live`. */
export function ActivityFeed({
  taskId,
  live,
  provider,
  className,
}: {
  taskId: string
  live: boolean
  provider?: string
  className?: string
}) {
  const { steps, exists, loaded, failed } = useTaskActivity(taskId, live)
  const listRef = useRef<HTMLOListElement | null>(null)

  // Stay at the newest step, unless the operator scrolled up to read.
  const pinned = useRef(true)
  useLayoutEffect(() => {
    const el = listRef.current
    if (el && pinned.current) el.scrollTop = el.scrollHeight
  }, [steps.length])

  let note: string | null = null
  if (failed) note = t("activity.load_failed")
  else if (loaded && !exists) note = t("activity.no_log")
  else if (loaded && steps.length === 0)
    note = provider && NOT_STREAMED.has(provider) ? t("activity.not_streamed", { provider }) : t("activity.empty")

  return (
    <div className={cn("font-mono text-[11px] leading-relaxed", className)}>
      {note ? <p className="text-muted-foreground">{note}</p> : null}
      {steps.length > 0 ? (
        <ol
          ref={listRef}
          aria-live={live ? "polite" : undefined}
          aria-label={t("activity.title")}
          onScroll={(e) => {
            const el = e.currentTarget
            pinned.current = el.scrollHeight - el.scrollTop - el.clientHeight < 8
          }}
          className="max-h-64 space-y-1 overflow-auto"
        >
          {steps.map((step, i) => (
            <StepRow key={i} step={step} />
          ))}
        </ol>
      ) : null}
    </div>
  )
}
