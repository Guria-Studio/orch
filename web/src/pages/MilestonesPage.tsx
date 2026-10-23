import { describeLoadError } from "@/lib/errors"
import { AlertTriangle } from "lucide-react"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Skeleton } from "@/components/ui/skeleton"
import { Explain } from "@/components/Explain"
import { useMilestones, type Milestone } from "@/hooks/useMilestones"
import { fmt, t, type MessageKey } from "@/i18n"
import { cn } from "@/lib/utils"

// A milestone's state borrows the task status scale: done is done, active runs,
// not started waits in the queue. `ring` colors the status circle and the label.
const MILESTONE_STATUS: Record<Milestone["status"], { label: MessageKey; ring: string; text: string }> = {
  done: { label: "milestones.status.done", ring: "border-status-done", text: "text-status-done" },
  active: { label: "milestones.status.active", ring: "border-status-running", text: "text-status-running" },
  pending: { label: "milestones.status.pending", ring: "border-status-queued", text: "text-muted-foreground" },
}

function Intro() {
  return (
    <header className="space-y-1">
      <h1 className="text-xl font-semibold tracking-[-0.01em]">{t("milestones.title")}</h1>
      <p className="max-w-3xl text-[13px] text-muted-foreground">{t("milestones.intro")}</p>
    </header>
  )
}

const pct = (n: number, total: number) => (total > 0 ? `${(n / total) * 100}%` : "0%")

// One phase as one row: name and state, a bar stacked done / running /
// blocked, and when it should finish.
function MilestoneRow({ milestone: m }: { milestone: Milestone }) {
  const status = MILESTONE_STATUS[m.status]
  const { done, total } = m.progress
  return (
    <li className="grid items-center gap-x-6 gap-y-2 p-4 md:grid-cols-[minmax(0,14rem)_minmax(0,1fr)_minmax(0,13rem)]">
      <div className="min-w-0 space-y-1">
        <h2 className="flex min-w-0 items-center gap-2 text-sm font-semibold">
          <span className={cn("h-3 w-3 shrink-0 rounded-full border-2", status.ring)} aria-hidden />
          <span className="truncate">{m.name}</span>
        </h2>
        <p className={cn("text-xs", status.text)}>{t(status.label)}</p>
      </div>
      <div className="min-w-0 space-y-2">
        <div
          className="flex h-1.5 overflow-hidden rounded-full bg-secondary"
          role="progressbar"
          aria-label={m.name}
          aria-valuemin={0}
          aria-valuemax={100}
          aria-valuenow={m.progress.pct}
        >
          <span className="h-full bg-status-done" style={{ width: pct(done, total) }} />
          <span className="h-full bg-status-running" style={{ width: pct(m.in_progress, total) }} />
          <span className="h-full bg-status-blocked" style={{ width: pct(m.blocked, total) }} />
        </div>
        <p className="flex flex-wrap gap-x-3 text-xs text-muted-foreground tabular-nums">
          <span>{t("milestones.tasks_done", { done, total })}</span>
          <span>{m.progress.pct}%</span>
          {m.blocked > 0 ? <span className="text-status-blocked">{t("milestones.blocked", { count: m.blocked })}</span> : null}
        </p>
      </div>
      <p className="text-xs leading-[18px] text-foreground/80">
        {m.status === "done" ? null : m.eta ? (
          <Explain term="eta">
            {t("milestones.eta", {
              date: fmt.day(m.eta.eta_date),
              confidence: t(m.eta.confidence === "high" ? "milestones.confidence.high" : "milestones.confidence.low"),
            })}
          </Explain>
        ) : (
          <span className="text-muted-foreground">{t("milestones.no_eta")}</span>
        )}
      </p>
    </li>
  )
}

export function MilestonesPage() {
  const { data: milestones, isLoading, isError, error } = useMilestones()

  if (isLoading) {
    return (
      <div className="space-y-6">
        <Intro />
        <Skeleton className="h-64 w-full rounded-lg" />
      </div>
    )
  }

  if (isError) {
    return (
      <Alert variant="destructive">
        <AlertTriangle className="h-4 w-4" />
        <AlertTitle>{t("milestones.load_failed")}</AlertTitle>
        <AlertDescription>{describeLoadError(error)}</AlertDescription>
      </Alert>
    )
  }

  if (!milestones || milestones.length === 0) {
    return (
      <div className="space-y-6">
        <Intro />
        <div className="rounded-lg border border-dashed p-5 text-muted-foreground">
          <p className="font-medium text-foreground">{t("milestones.none")}</p>
          <p className="mt-1">
            {t("milestones.none_before")} <code className="font-mono">orch atomize --apply</code>.
          </p>
        </div>
      </div>
    )
  }

  return (
    <div className="space-y-4 text-[13px]">
      <Intro />
      <ul className="divide-y overflow-hidden rounded-lg border bg-card" aria-label={t("milestones.title")}>
        {milestones.map((m) => (
          <MilestoneRow key={m.phase} milestone={m} />
        ))}
      </ul>
    </div>
  )
}
