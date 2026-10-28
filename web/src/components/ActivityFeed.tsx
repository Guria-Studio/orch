import { useLayoutEffect, useRef, useState } from "react"
import { AlertTriangle, ArrowDown, CheckCircle2, MessageSquare, Play, Wrench } from "lucide-react"
import { useTaskActivity, type ActivityStep } from "@/hooks/useTaskActivity"
import { t } from "@/i18n"
import { cn } from "@/lib/utils"

// gemini and agy print one object when they finish: nothing to follow before.
const NOT_STREAMED = new Set(["gemini", "agy"])

// A one-line row truncates in a card; on the task's page (`wrap`) it wraps, so a
// long tool call reads in full without the page scrolling sideways.
export function StepRow({ step, wrap }: { step: ActivityStep; wrap: boolean }) {
  const long = wrap ? "whitespace-pre-wrap break-all" : "truncate"
  switch (step.kind) {
    case "tool":
      return (
        <li className="flex items-baseline gap-2">
          <Wrench className="h-3 w-3 shrink-0 translate-y-0.5 text-status-running" aria-hidden />
          <span className="shrink-0 font-medium text-foreground">{step.tool}</span>
          <span className={cn("min-w-0 text-muted-foreground", long)} title={step.detail}>
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
          <span className={cn("min-w-0", long)} title={step.detail}>
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
  full = false,
  wrap = full,
}: {
  taskId: string
  live: boolean
  provider?: string
  className?: string
  // The task's own page: the feed fills the viewport and its rows wrap.
  full?: boolean
  // Rows wrap without the feed filling the viewport (the task page's All tab).
  wrap?: boolean
}) {
  const { steps, exists, loaded, failed } = useTaskActivity(taskId, live)
  const listRef = useRef<HTMLOListElement | null>(null)

  // Stay at the newest step, unless the operator scrolled up to read.
  const pinned = useRef(true)
  const [behind, setBehind] = useState(false)
  useLayoutEffect(() => {
    const el = listRef.current
    if (el && pinned.current) el.scrollTop = el.scrollHeight
  }, [steps.length])
  const jumpToLatest = () => {
    const el = listRef.current
    if (el) el.scrollTop = el.scrollHeight
    pinned.current = true
    setBehind(false)
  }

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
            if (behind === pinned.current) setBehind(!pinned.current)
          }}
          className={cn("space-y-1 overflow-auto", full ? "h-[calc(100dvh-12rem)] min-h-64" : "max-h-64")}
        >
          {steps.map((step, i) => (
            <StepRow key={i} step={step} wrap={wrap} />
          ))}
        </ol>
      ) : null}
      {live && behind ? (
        <button
          type="button"
          onClick={jumpToLatest}
          className="mt-2 inline-flex items-center gap-1 rounded-md border bg-background px-2 py-1 font-sans text-xs hover:bg-accent"
        >
          <ArrowDown className="h-3 w-3" aria-hidden />
          {t("activity.jump_latest")}
        </button>
      ) : null}
    </div>
  )
}
